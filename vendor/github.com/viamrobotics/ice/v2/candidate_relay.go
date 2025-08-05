// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

package ice

import (
	"net"
	"time"

	"github.com/pion/turn/v2"
)

// CandidateRelay ...
type CandidateRelay struct {
	candidateBase

	relayProtocol   string
	turnControlConn *turn.Client
	onClose         func() error
}

// CandidateRelayConfig is the config required to create a new CandidateRelay
type CandidateRelayConfig struct {
	CandidateID     string
	Network         string
	Address         string
	Port            int
	Component       uint16
	Priority        uint32
	Foundation      string
	RelAddr         string
	RelPort         int
	RelayProtocol   string
	TURNControlConn *turn.Client
	TCPType         TCPType
	OnClose         func() error
}

// NewCandidateRelay creates a new relay candidate
func NewCandidateRelay(config *CandidateRelayConfig) (*CandidateRelay, error) {
	candidateID := config.CandidateID

	if candidateID == "" {
		candidateID = globalCandidateIDGenerator.Generate()
	}

	ip := net.ParseIP(config.Address)
	if ip == nil {
		return nil, ErrAddressParseFailed
	}

	networkType, err := determineNetworkType(config.Network, ip)
	if err != nil {
		return nil, err
	}

	var resolvedAddr net.Addr
	if networkType.IsUDP() {
		resolvedAddr = net.Addr(&net.UDPAddr{IP: ip, Port: config.Port})
	} else if networkType.IsTCP() {
		resolvedAddr = &net.TCPAddr{IP: ip, Port: config.Port}
	}

	return &CandidateRelay{
		candidateBase: candidateBase{
			id:                 candidateID,
			createdAt:          time.Now(),
			networkType:        networkType,
			candidateType:      CandidateTypeRelay,
			address:            config.Address,
			port:               config.Port,
			resolvedAddr:       resolvedAddr,
			component:          config.Component,
			foundationOverride: config.Foundation,
			priorityOverride:   config.Priority,
			relatedAddress: &CandidateRelatedAddress{
				Address: config.RelAddr,
				Port:    config.RelPort,
			},
			remoteCandidateCaches: map[AddrPort]Candidate{},
			tcpType:               config.TCPType,
		},
		relayProtocol:   config.RelayProtocol,
		turnControlConn: config.TURNControlConn,
		onClose:         config.OnClose,
	}, nil
}

// LocalPreference returns the local preference for this candidate
func (c *CandidateRelay) LocalPreference() uint16 {
	// These preference values come from libwebrtc
	// https://github.com/mozilla/libwebrtc/blob/1389c76d9c79839a2ca069df1db48aa3f2e6a1ac/p2p/base/turn_port.cc#L61
	var relayPreference uint16
	switch c.relayProtocol {
	case relayProtocolTLS, relayProtocolDTLS:
		relayPreference = 2
	case tcp:
		relayPreference = 1
	default:
		relayPreference = 0
	}

	return c.candidateBase.LocalPreference() + relayPreference
}

// RelayProtocol returns the protocol used between the endpoint and the relay server.
func (c *CandidateRelay) RelayProtocol() string {
	return c.relayProtocol
}

func (c *CandidateRelay) close() error {
	err := c.candidateBase.close()
	if c.onClose != nil {
		err = c.onClose()
		c.onClose = nil
	}
	return err
}

func (c *CandidateRelay) copy() (Candidate, error) {
	cc, err := c.candidateBase.copy()
	if err != nil {
		return nil, err
	}

	if ccr, ok := cc.(*CandidateRelay); ok {
		ccr.relayProtocol = c.relayProtocol
	}

	return cc, nil
}

// CreatePermission
func (c *CandidateRelay) CreatePermission(peerAddr net.Addr) error {
	return c.turnControlConn.CreatePermission(peerAddr)
}
