package presence

import (
	"log"
	"net"
	"strings"
	"time"

	"github.com/miekg/dns"
)

var hosts map[string]*Host = map[string]*Host{}

type Host struct {
	Name    string
	Address net.IP
	Status  bool
}

func (h *Host) ping(ch chan Host) {
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
				h.Status = true
				ch <- *h
				return
			}
		case <-timeout:
			break
		}
	}
	h.Status = false
	ch <- *h
}

func Listen(current map[string]bool) chan Host {
	for name, _ := range current {
		hosts[name] = &Host{Name: name}
		if a, err := net.ResolveTCPAddr("tcp4", name+".local:80"); err == nil {
			hosts[name].Address = a.IP
		}
	}

	ch := make(chan Host)
	mcaddr, err := net.ResolveUDPAddr("udp", "224.0.0.251:5353") // mdns/bonjour
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.ListenMulticastUDP("udp", nil, mcaddr)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
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
							h, ok := hosts[name]
							if !ok {
								h = &Host{Name: name, Address: rr.(*dns.A).A}
								hosts[name] = h
							}
							h.Address = rr.(*dns.A).A
							go h.ping(ch)
						}
					}
				}
			} else {
				log.Println("err:", err)
			}
		}
	}()

	go func() {
		c := time.Tick(60 * time.Second)
		for {
			select {
			case <-c:
				for _, h := range hosts {
					go h.ping(ch)
				}
			}
		}
	}()

	return ch
}
