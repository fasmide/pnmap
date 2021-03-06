package main

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"net/http"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type intel struct {
	NICCollection map[string]*NIC
	hostChan      chan *NIC

	mux mux
}

func newIntel() *intel {
	i := &intel{
		NICCollection: make(map[string]*NIC),
		mux:           newMux(),
	}

	i.mux.add(layers.LayerTypeARP, i.arp)
	i.mux.add(layers.LayerTypeDHCPv4, i.dhcpv4)
	i.mux.add(layers.LayerTypeIPv4, i.ipv4)
	i.mux.add(layers.LayerTypeIPv6, i.ipv6)
	i.mux.add(layers.LayerTypeUDP, i.ssdp)

	return i
}

func (i *intel) getNIC(addr []byte) *NIC {
	mac := mac(addr)

	nic, found := i.NICCollection[mac]
	if !found {
		n := newNIC(addr)
		i.NICCollection[mac] = n
		nic = n
	}

	i.hostChan <- nic

	return nic
}

func (i *intel) dhcpv4(source net.HardwareAddr, layer gopacket.Layer) {
	dhcpv4 := layer.(*layers.DHCPv4)
	if dhcpv4.Operation != layers.DHCPOpRequest {
		return
	}

	for _, o := range dhcpv4.Options {
		nic := i.getNIC(source)

		switch o.Type {
		case layers.DHCPOptClassID:
			nic.vendor.add(string(o.Data))
		case layers.DHCPOptHostname:
			nic.Hostnames.add(string(o.Data))
		}
	}
}

func (i *intel) arp(source net.HardwareAddr, layer gopacket.Layer) {
	arp := layer.(*layers.ARP)

	nic := i.getNIC(source)

	if len(arp.SourceProtAddress) == 4 {
		ip := fmt.Sprintf("%d.%d.%d.%d", arp.SourceProtAddress[0], arp.SourceProtAddress[1], arp.SourceProtAddress[2], arp.SourceProtAddress[3])
		if ip != "0.0.0.0" {
			nic.IPs.add(ip)
		}
	}
}

func (i *intel) ipv6(source net.HardwareAddr, layer gopacket.Layer) {
	ipv6 := layer.(*layers.IPv6)

	nic := i.getNIC(source)

	if ip := ipv6.SrcIP.String(); ip != "::" {
		nic.IPs.add(ip)
	}
}

func (i *intel) ipv4(source net.HardwareAddr, layer gopacket.Layer) {
	ipv4 := layer.(*layers.IPv4)

	nic := i.getNIC(source)

	ip := ipv4.SrcIP.String()
	if ip == "0.0.0.0" {
		return
	}

	nic.IPs.add(ip)
}

func (i *intel) ssdp(source net.HardwareAddr, layer gopacket.Layer) {
	udp := layer.(*layers.UDP)

	if udp.DstPort == 1900 {
		req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(udp.Payload)))
		if err != nil {
			return
		}

		ua := req.Header.Get("user-agent")
		if ua != "" {
			nic := i.getNIC(source)
			nic.userAgents.add(ua)
		}
	}
}

func (i *intel) NewPacket(packet gopacket.Packet) {
	if ethernetLayer := packet.Layer(layers.LayerTypeEthernet); ethernetLayer != nil {
		ethernet := ethernetLayer.(*layers.Ethernet)

		nic := i.getNIC(ethernet.SrcMAC)

		nic.lastSeen = packet.Metadata().Timestamp
		nic.seen++

		i.mux.process(packet)
	}
}
