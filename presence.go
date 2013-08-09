package presence

import (
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

var hosts map[string]*Host = map[string]*Host{}

type Presence struct {
	Name   string
	Status bool
}

type Host struct {
	Name    string
	Address net.IP
}

func (h Host) ping(ch chan Presence) {
	c := make(chan bool)

	ports := []string{"22", "62078"}

	for _, port := range ports {
		go func(port string) {
			conn, err := net.DialTimeout("tcp", h.Address.String()+":"+port, 10*time.Second)
			if err == nil {
				c <- true
				conn.Close()
			} else {
				c <- false
			}
		}(port)
	}

	timeout := time.After(10 * time.Second)

	for i := 0; i < len(ports); i++ {
		select {
		case r := <-c:
			if r == true {
				ch <- Presence{h.Name, true}
				return
			}
		case <-timeout:
			break
		}
	}
	ch <- Presence{h.Name, false}
}

func Listen(current map[string]bool) chan Presence {
	for name, _ := range current {
		hosts[name] = &Host{Name: name}
		if a, err := net.ResolveTCPAddr("tcp4", name+".local:80"); err == nil {
			hosts[name].Address = a.IP
		}
	}

	hostch := make(chan Host)

	go func() {
		mcaddr, err := net.ResolveUDPAddr("udp", "224.0.0.251:5353") // mdns/bonjour
		if err != nil {
			log.Fatal(err)
		}
		conn, err := net.ListenMulticastUDP("udp", nil, mcaddr)
		if err != nil {
			log.Fatal(err)
		}

		for {
			buf := make([]byte, dns.MaxMsgSize)
			read, _, err := conn.ReadFromUDP(buf)
			if err != nil {
				log.Println("err:", err)
			}
			var msg dns.Msg
			if err := msg.Unpack(buf[:read]); err == nil {
				if msg.MsgHdr.Response {
					for i := 0; i < len(msg.Answer); i++ {
						rr := msg.Answer[i]
						rh := rr.Header()
						if rh.Rrtype == dns.TypeA {
							name := strings.NewReplacer(".local.", "").Replace(rh.Name)
							hostch <- Host{name, rr.(*dns.A).A}
						}
					}
				}
			} else {
				log.Println("err:", err)
			}
		}
	}()

	ch := make(chan Presence)
	go func() {
		c := time.Tick(60 * time.Second)
		for {
			select {
			case host := <-hostch:
				h, ok := hosts[host.Name]
				if !ok {
					h = &Host{Name: host.Name, Address: host.Address}
					hosts[host.Name] = h
				}
				h.Address = host.Address
				go h.ping(ch)
			case <-c:
				for _, h := range hosts {
					go h.ping(ch)
				}
			}
		}
	}()

	return ch
}
