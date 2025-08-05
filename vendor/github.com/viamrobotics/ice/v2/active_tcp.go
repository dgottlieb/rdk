// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ice

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v2/packetio"
)

type activeTCPConn struct {
	// An `activeTCPConn` may create multiple `conn` objects. When a write to a `conn` fails, we
	// will redial. And communicate the new connection to the reader via this atomic pointer. The
	// pointer may be nil as the original dial may fail.
	//
	// Dan: Once a tcp candidate has been chosen, it's unclear to me that redialing forever is
	// correct. When a redial happens, packets may have been lost. WebRTC data channels can be
	// configured to be "reliable".
	//
	// Clearly if a candidate is using UDP under the hood, data channels must manage reliable and
	// ordered packet delivery. So dropped packets in that scenario is safe.
	//
	// However, it's unclear to me that data channels running on top of TCP candidates are privy to
	// the TCP connection. If they are, it's plausible that data channel implementations can make
	// assumptions that reliable packet delivery is being guaranteed by a TCP candidate. And
	// therefore optimize away the any data channel reliable delivery mechanisms.
	conn                    atomic.Pointer[net.Conn]
	readBuffer, writeBuffer *packetio.Buffer
	closed                  int32
}

// Dan: `localAddress` (specifically the port) I believe is non-functional. Being we dial out to the
// other peer, there's no need to "listen" on our assigned port.
func newActiveTCPConn(ctx context.Context, localAddress, remoteAddress string, log logging.LeveledLogger) (a *activeTCPConn) {
	a = &activeTCPConn{
		readBuffer:  packetio.NewBuffer(),
		writeBuffer: packetio.NewBuffer(),
	}

	// On error, `laddr` will be nil, which is fine for the dialer.
	laddr, err := getTCPAddrOnInterface(localAddress)
	if err != nil {
		log.Infof("Failed to listen on TCP address. Assuming srflx %s: %v", localAddress, err)
	}

	// Spin off a goroutine that will:
	// - Spin off a second goroutine to adopt the role of the "read loop".
	// - Become the "write loop". goroutine.
	go func() {
		dialer := &net.Dialer{
			LocalAddr: laddr,
		}

		// Create an initial connection object and (ideally) initialize the atomic pointer to it.
		if conn, err := dialer.DialContext(ctx, "tcp", remoteAddress); err == nil {
			// The connection may get recreated, particularly during the connection process. The
			// "write" loop owns redialing and communicates to the "read" loop via this
			// `activeTCPConn.conn` member variable.
			a.conn.Store(&conn)
		} else {
			log.Infof("Failed to dial TCP address %s: %v", remoteAddress, err)
		}

		defer func() {
			// This outer goroutine represents the "write" loop. When the write loop exits, we will
			// consider the connection closed. And signal the "read" loop to exit.
			atomic.StoreInt32(&a.closed, 1)
			connPtr := a.conn.Load()
			if connPtr != nil {
				(*connPtr).Close()
			}
		}()

		// Spin up a read loop. The loop reads from the connection and writes payloads to the
		// `readBuffer`.
		go func() {
			buff := make([]byte, receiveMTU)

			for atomic.LoadInt32(&a.closed) == 0 {
				// The write loop may encounter errors and re-dial the destination. Always refresh
				// our pointer to the connection to read from.
				connPtr := a.conn.Load()
				if connPtr == nil {
					time.Sleep(10 * time.Millisecond)
					continue
				}

				n, err := readStreamingPacket(*connPtr, buff)
				if err != nil {
					log.Debugf("Failed to read streaming packet: %s", err)
					time.Sleep(10 * time.Millisecond)
					continue
				}

				if _, err := a.readBuffer.Write(buff[:n]); err != nil {
					// The `readBuffer` has a 1:1 lifetime with respect to the `activeTCPConn`. If
					// this in-memory operation fails, we can only bail.
					log.Warnf("Failed to write to buffer: %s", err)
					break
				}
			}
		}()

		connPtr := a.conn.Load()
		buff := make([]byte, receiveMTU)
		for atomic.LoadInt32(&a.closed) == 0 {
			toWrite, err := a.writeBuffer.Read(buff)
			if err != nil {
				// The `writeBuffer` has a 1:1 lifetime with respect to the `activeTCPConn`. If
				// this in-memory operation fails, we can only bail.
				log.Warnf("Failed to read from buffer: %s", err)
				break
			}

			if connPtr != nil {
				// If we have a connection, write the bytes. If not, we'll drop these bytes. We
				// assume that's ok from the perspective that the higher level is guaranteeing
				// order/delivery when needed.
				//
				// More broadly, we expect connections getting swapped out during the
				// gathering/connecting phase. And not after a connection was established and has
				// perhaps been selected as the candidate pair for communication.
				_, err = writeStreamingPacket(*connPtr, buff[:toWrite])
			}

			// If we had a connection and writing bytes did not result in an error. We're
			// good. Otherwise reconnect.
			if connPtr != nil && err == nil {
				continue
			}

			if connPtr == nil {
				log.Debugf("Failed to write streaming packet. Nil conn.")
			} else {
				log.Debugf("Failed to write streaming packet. Redialing: %s", err)
			}

			if connPtr != nil {
				(*connPtr).Close()
			}

			newConn, err := dialer.DialContext(ctx, "tcp", remoteAddress)
			if err != nil {
				log.Debugf("Failed to dial TCP address %s: %v", remoteAddress, err)
				continue
			}

			connPtr = &newConn
			a.conn.Store(connPtr)
		}
	}()

	return a
}

func (a *activeTCPConn) ReadFrom(buff []byte) (n int, srcAddr net.Addr, err error) {
	if atomic.LoadInt32(&a.closed) == 1 {
		return 0, nil, io.ErrClosedPipe
	}

	srcAddr = a.RemoteAddr()
	n, err = a.readBuffer.Read(buff)
	return
}

func (a *activeTCPConn) WriteTo(buff []byte, _ net.Addr) (n int, err error) {
	if atomic.LoadInt32(&a.closed) == 1 {
		return 0, io.ErrClosedPipe
	}

	return a.writeBuffer.Write(buff)
}

func (a *activeTCPConn) Close() error {
	atomic.StoreInt32(&a.closed, 1)
	_ = a.readBuffer.Close()
	_ = a.writeBuffer.Close()
	return nil
}

func (a *activeTCPConn) LocalAddr() net.Addr {
	connPtr := a.conn.Load()
	if connPtr == nil {
		return &net.TCPAddr{}
	}

	if v, ok := (*connPtr).LocalAddr().(*net.TCPAddr); ok {
		return v
	}

	return &net.TCPAddr{}
}

func (a *activeTCPConn) RemoteAddr() net.Addr {
	connPtr := a.conn.Load()
	if connPtr == nil {
		return &net.TCPAddr{}
	}

	if v, ok := (*connPtr).RemoteAddr().(*net.TCPAddr); ok {
		return v
	}

	return &net.TCPAddr{}
}

func (a *activeTCPConn) SetDeadline(time.Time) error      { return io.EOF }
func (a *activeTCPConn) SetReadDeadline(time.Time) error  { return io.EOF }
func (a *activeTCPConn) SetWriteDeadline(time.Time) error { return io.EOF }

func getTCPAddrOnInterface(address string) (*net.TCPAddr, error) {
	addr, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return nil, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = l.Close()
	}()

	tcpAddr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return nil, errInvalidAddress
	}

	return tcpAddr, nil
}
