package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"syscall"
	"time"

	pcapreader "github.com/evnix/pcap-reader"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/mdlayher/raw"
	"github.com/spf13/cobra"
)

var (
	rootCmd = &cobra.Command{
		Use: os.Args[0],
	}

	interfaces *[]string

	hostInterfaces []net.Interface
)

func init() {
	hostInterfaces, _ = net.Interfaces()

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List network interfaces",
		Run:   list,
	}
	rootCmd.AddCommand(listCmd)

	monitorCmd := &cobra.Command{
		Use:   "monitor",
		Short: "Monitor all interfaces for Probe Requests",
		Run:   monitor,
	}
	rootCmd.AddCommand(monitorCmd)

	simulateCmd := &cobra.Command{
		Use:   "simulate",
		Short: "",
		Run:   simulate,
		Args:  cobra.ExactArgs(1),
	}
	rootCmd.AddCommand(simulateCmd)

	interfaces = monitorCmd.PersistentFlags().StringArrayP("interface", "i", []string{"all"}, "Interface(s) to monitor")
}

func list(_ *cobra.Command, _ []string) {
	for _, i := range hostInterfaces {
		fmt.Printf("%s\n", i.Name)
	}

	os.Exit(0)
}

func listen(deviceName string, out chan gopacket.Packet) {
	intf, err := net.InterfaceByName(deviceName)
	if err != nil {
		log.Printf("error: %s", err.Error())
	}

	conn, err := raw.ListenPacket(intf, syscall.ETH_P_ALL, nil)
	if err != nil {
		log.Printf("error: %s", err.Error())
	}

	buffer := make([]byte, 65536)

	for {
		_, addr, err := conn.ReadFrom(buffer)
		if err != nil {
			log.Fatalf("error: %s", err.Error())
			break
		}

		// Throw away packets with no source.
		if addr.String() == "00:00:00:00:00:00" {
			continue
		}

		packet := gopacket.NewPacket(buffer, layers.LayerTypeEthernet, gopacket.Default)
		packet.Metadata().Timestamp = time.Now()

		out <- packet
	}
}

func monitor(_ *cobra.Command, _ []string) {
	packets := make(chan gopacket.Packet, 10)

	if len(*interfaces) == 1 && (*interfaces)[0] == "all" {
		*interfaces = []string{}

		for _, i := range hostInterfaces {
			*interfaces = append(*interfaces, i.Name)
		}
	}

	for _, i := range *interfaces {
		go listen(i, packets)
	}

	i := newIntel()
	g := newGUI()

	i.hostChan = make(chan *NIC, 10)

	go func() {
		for packet := range packets {
			i.NewPacket(packet)
		}
	}()

	go func() {
		for nic := range i.hostChan {
			g.updateNIC(nic)
		}
	}()

	_ = g.Run()
}

func simulate(_ *cobra.Command, args []string) {
	packets := make(chan gopacket.Packet, 10)

	reader := pcapreader.PCapReader{}
	err := reader.Open(args[0])
	if err != nil {
		log.Fatalf(err.Error())
	}
	defer reader.Close()

	i := newIntel()
	g := newGUI()

	i.hostChan = make(chan *NIC, 10)

	go func() {
		for {
			header, data, err := reader.ReadNextPacket()
			if err == io.EOF {
				break
			}

			packet := gopacket.NewPacket(data, layers.LayerTypeEthernet, gopacket.Default)
			packet.Metadata().Timestamp = time.Unix(int64(header.TsSec), int64(header.TsUsec)*1000)
			packets <- packet
		}
	}()

	go func() {
		for {
			select {
			case packet := <-packets:
				go i.NewPacket(packet)
			case nic := <-i.hostChan:
				g.updateNIC(nic)
			}
		}
	}()

	err = g.Run()
	if err != nil {
		log.Fatal(err.Error())
	}
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		log.Fatal(err.Error())
	}
}
