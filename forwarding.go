package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"sync"
)

const (
	TCP = "tcp"
	UDP = "udp"
)

type Port struct {
	SourcePort int
	DestPort   int
	Protocol   string
}

type ProxyService struct {
	DestAddress string
	Ports       []Port
}

func NewService(ports []Port) *ProxyService {
	return &ProxyService{Ports: ports}
}

func (l *ProxyService) Start() error {
	for _, port := range l.Ports {
		channel := getChannel(port.Protocol)
		go channel.Serve(l.DestAddress, port)
	}
	return nil
}

type Channel interface {
	Serve(address string, ports Port)
}

func getChannel(protocol string) Channel {
	switch protocol {
	case TCP:
		return new(TCPChannel)
	case UDP:
		return new(UDPChannel)
	}
	return nil
}

type TCPChannel struct {
	ports Port
	addr  string
}

type UDPChannel struct {
	TCPChannel
	connections map[string]*net.UDPAddr
	targetAddr  *net.UDPAddr
	targetConn  *net.UDPConn
	proxyConn   *net.UDPConn
	lock        sync.Mutex
}

func (l *TCPChannel) Serve(address string, ports Port) {

	incoming, err := net.Listen("tcp", string(l.ports.SourcePort))
	if err != nil {
		log.Fatalf("failed to serve on %d:%v", l.ports.SourcePort, err)
	}

	fmt.Printf("serving on port: %d\n", l.ports.SourcePort)

	client, err := incoming.Accept()
	if err != nil {
		log.Fatal("failed to accept client connection", err)
	}
	defer client.Close()
	fmt.Println("connected to client", client.RemoteAddr())

	target, err := net.Dial("tcp", fmt.Sprintf("%s:%d", l.addr, l.ports.DestPort))
	if err != nil {
		log.Fatal("failed to connect to target", err)
	}
	defer target.Close()
	fmt.Println("connected to target at ", target.RemoteAddr())

	// start copy threads
	go func() { io.Copy(target, client) }()
	go func() { io.Copy(client, target) }()
}

func (l *UDPChannel) forwardTargetToClient(clientAddr *net.UDPAddr) {

	var buffer [1500]byte
	for {
		// Read from target
		n, err := l.targetConn.Read(buffer[0:])
		if err != nil {
			continue
		}
		// write to the client
		_, err = l.proxyConn.WriteToUDP(buffer[0:n], clientAddr)
		if err != nil {
			continue
		}
	}
}

func (l *UDPChannel) handleForwarding() {
	var buffer [1500]byte
	for {
		n, clientAddr, err := l.proxyConn.ReadFromUDP(buffer[0:])
		if err != nil {
			continue
		}

		l.lock.Lock()
		if clientAddr, present := l.connections[clientAddr.String()]; !present {
			// add a new clients
			//TODO: limit the number of cliens?
			if clientAddr != nil {
				l.connections[clientAddr.String()] = clientAddr

				go l.forwardTargetToClient(clientAddr)
			}
		}
		l.lock.Unlock()

		// write to the target
		_, err = l.targetConn.Write(buffer[0:n])
		if err != nil {
			continue
		}
	}
}
func (l *UDPChannel) Serve(address string, port Port) {
	l.addr = address
	l.ports = port

	localAddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}

	// store destination address
	l.targetAddr, err = net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", l.addr, l.ports.DestPort))
	if err != nil {
		panic(err)
	}

	l.proxyConn, err = net.ListenUDP("udp", localAddr)
	if err != nil {
		panic(err)
	}
	// Start the forwarding threads and account for connected clients
	go l.handleForwarding()
}
