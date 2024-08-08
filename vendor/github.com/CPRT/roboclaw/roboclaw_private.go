package roboclaw

import (
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

type crcType uint16

/*
 * Variadic function to update the crc16 value
 * @param data ([]uint8) the bytes to update the crc
 */
func (crc *crcType) update(data ...uint8) {
	for _, n := range data {
		*crc = *crc ^ (crcType(n) << 8)
		for i := 0; i < 8; i++ {
			if (*crc & 0x8000) != 0 {
				*crc = (*crc << 1) ^ 0x1021
			} else {
				*crc <<= 1
			}
		}
	}
}

/*
 * Writes a number of bytes to the given roboclaw address
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @param vals ([]uint8) any additional values to transmit
 * @returns (error) transmission success or failure
 */
func (r *Roboclaw) write_n(address uint8, cmd uint8, vals ...uint8) error {

	var (
		crc crcType = crcType(0)
		buf [1]byte
		err error
	)
	vals = append([]uint8{address, cmd}, vals...)
	crc.update(vals...)
	vals = append(vals, uint8(0xFF&(crc>>8)), uint8(0xFF&crc))

	for trys := r.retries; trys > 0; trys-- {
		// Empty error (this should never be returned to user, but is employed in case of error in control flow logic)
		// That way the only way nil is returned is if there is success
		err = fmt.Errorf("Assert: This error should be replaced by other errors or nil")
		if err = r.port.Flush(); nil != err {
			continue
		} else if _, err = r.port.Write(vals); nil != err {
			continue
		}

		// If the writeSleep flag is true, then
		// instead of waiting for a reply simply sleep for the expected
		// timeout (10 milliseconds)
		// This can resolve issues if the rx line is broken but
		// the tx line is still operational, as the extra delay from
		// the wait will no longer be a barrier to efficient communication
		// with the roboclaw
		if r.writeSleep {
			time.Sleep(time.Millisecond * 10)
			return nil
		} else if _, err = io.ReadFull(r.port, buf[:]); err != nil {
			continue
		} else if buf[0] == 0xFF {
			return nil
		} else {
			err = fmt.Errorf("Incorrect Response Byte")
		}
	}
	return err
}

/*
 * Read a variable number of uint32 from the roboclaw
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @param vals ([]*uint32) pointers to the ints that are being read from the bus
 * @returns (error) transmission success or failure
 */
func (r *Roboclaw) read_n(address uint8, cmd uint8, vals ...*uint32) error {
	var (
		buf  [4]uint8
		crc  crcType
		ccrc crcType
		err  error
	)
loop:
	for trys := r.retries; trys > 0; trys-- {
		// Empty error (this should never be returned to user, but is employed in case of error in control flow logic)
		// That way the only way nil is returned is if there is success
		err = fmt.Errorf("Assert: This error should be replaced by other errors or nil")
		if err = r.port.Flush(); nil != err {
			continue
		}

		crc = crcType(0)

		crc.update(address, cmd)
		if _, err = r.port.Write([]uint8{address, cmd}); nil != err {
			continue
		}

		for _, val := range vals {
			if _, err = io.ReadFull(r.port, buf[:]); err != nil {
				continue loop
			} else {
				crc.update(buf[:]...)
				//Format each array of four bytes into the int pointer
				*val = binary.BigEndian.Uint32(buf[:])
			}
		}

		if _, err = io.ReadFull(r.port, buf[:2]); err == nil {
			ccrc = crcType(buf[0]) << 8
			ccrc |= crcType(buf[1])

			if ccrc == crc {
				return nil
			} else {
				err = fmt.Errorf("Mismatched checksum values")
			}
		}
	}
	return err
}

/*
 * Read 1 byte from the roboclaw
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @returns (uint8, error) the returned byte and any error
 */
func (r *Roboclaw) read1(address uint8, cmd uint8) (uint8, error) {
	n, err := r.readCount(1, address, cmd)
	return n[0], err
}

/*
 * Read 1 16 bit short from the roboclaw
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @returns (uint16, error) the returned short and any error
 */
func (r *Roboclaw) read2(address uint8, cmd uint8) (uint16, error) {
	n, err := r.readCount(2, address, cmd)
	return binary.BigEndian.Uint16(n), err
}

/*
 * Read 1 32 bit int from the roboclaw
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @returns (uint32, error) the returned int and any error
 */
func (r *Roboclaw) read4(address uint8, cmd uint8) (uint32, error) {
	n, err := r.readCount(4, address, cmd)
	return binary.BigEndian.Uint32(n), err
}

/*
 * Read 2 32 bit ints from the roboclaw
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @returns (uint32, uint32, error) the returned ints and any error
 */
func (r *Roboclaw) read4_4(address uint8, cmd uint8) (uint32, uint32, error) {
	n, err := r.readCount(8, address, cmd)
	return binary.BigEndian.Uint32(n[0:4]), binary.BigEndian.Uint32(n[4:8]), err
}

/*
 * Read four bytes from the roboclaw along with a status flag.
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @returns (uint32, uint8, error) the returned bytes formatted into a single int and a status byte and any error
 */
func (r *Roboclaw) read4_1(address uint8, cmd uint8) (uint32, uint8, error) {
	n, err := r.readCount(5, address, cmd)
	return binary.BigEndian.Uint32(n[0:4]), n[4], err
}

/*
 * Read the specified number of bytes from the roboclaw.
 * Only 1, 2, or 4 bytes may be read. Any other values trigger a panic.
 * @param count (uint8) the number of bytes to read
 * @param address (uint8) the roboclaw address
 * @param cmd (uint8) the command number
 * @returns ([]uint8, error) the returned bytes and any error
 */
func (r *Roboclaw) readCount(count uint8, address uint8, cmd uint8) ([]uint8, error) {

	var (
		buf  []uint8
		crc  crcType
		ccrc crcType
		err  error
	)

	//Only allowed values are 1, 2, and 4
	if count != 1 && count != 2 && count != 4 && count != 5 && count != 8 {
		panic("Cannot read count for values other than 1, 2, 4, 5, and 8")
	}

	for trys := r.retries; trys > 0; trys-- {
		// Empty error (this should never be returned to user, but is employed in case of error in control flow logic)
		// That way the only way nil is returned is if there is success
		err = fmt.Errorf("Assert: This error should be replaced by other errors or nil")
		if err = r.port.Flush(); err != nil {
			continue
		}
		crc = crcType(0)

		crc.update(address, cmd)
		if _, err = r.port.Write([]uint8{address, cmd}); err != nil {
			continue
		}

		//Create the appropriate buffer size depending on how
		// many bytes will be read
		buf = make([]uint8, count+2)
		if _, err = io.ReadFull(r.port, buf); err == nil {

			//The final two bytes are the crc from the motor controller
			ccrc = crcType(buf[count]) << 8
			ccrc |= crcType(buf[count+1])

			//Update the crc with the data bytes
			crc.update(buf[0:count]...)

			if ccrc == crc {
				return buf[0:count], nil
			} else {
				err = fmt.Errorf("Mismatched checksum values")
			}
		}
	}
	return make([]uint8, count), err
}
