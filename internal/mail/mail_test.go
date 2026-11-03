package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeRelay accepts one connection. With stall set it never answers, like a
// relay that hangs; otherwise it speaks just enough SMTP to accept a message.
func fakeRelay(t *testing.T, stall bool) (port int, got chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		if stall {
			time.Sleep(5 * time.Second)
			return
		}
		r := bufio.NewReader(c)
		say := func(s string) { c.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		var body strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					say("250 queued")
					got <- body.String()
					continue
				}
				body.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				say("250 ok")
			case cmd == "DATA":
				inData = true
				say("354 go")
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

func TestSMTPSendDelivers(t *testing.T) {
	port, got := fakeRelay(t, false)
	s := SMTP{Host: "127.0.0.1", Port: port, From: "Kork <hi@example.com>"}
	if err := s.Send(context.Background(), Message{To: "fan@example.org", Subject: "Hi", Body: "Hello\nthere"}); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-got:
		if !strings.Contains(body, "Subject: Hi") || !strings.Contains(body, "Hello\r\nthere") {
			t.Fatalf("body: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay got nothing")
	}
}

// A relay that accepts the connection and then stalls must not hang Send.
func TestSMTPSendHonorsDeadline(t *testing.T) {
	port, _ := fakeRelay(t, true)
	s := SMTP{Host: "127.0.0.1", Port: port, From: "hi@example.com"}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := s.Send(ctx, Message{To: "fan@example.org", Subject: "Hi", Body: "x"})
	if err == nil {
		t.Fatal("send to a stalled relay succeeded")
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("send took %v", took)
	}
}
