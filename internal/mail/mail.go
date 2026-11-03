// Package mail sends account emails over SMTP, or logs them in development.
package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

type Message struct {
	To      string
	Subject string
	Body    string
	// Headers are extra header lines, such as List-Unsubscribe.
	Headers map[string]string
}

type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Log writes messages to the log instead of sending them.
type Log struct{ Logger *slog.Logger }

func (l Log) Send(_ context.Context, m Message) error {
	l.Logger.Info("mail (not sent: SMTP not configured)", "to", m.To, "subject", m.Subject, "body", m.Body)
	return nil
}

// SMTP sends through an SMTP relay using STARTTLS when offered.
type SMTP struct {
	Host, User, Pass, From string
	Port                   int
}

// DefaultTimeout bounds a send when the context has no deadline.
const DefaultTimeout = 30 * time.Second

// Send delivers m. Dialing, every read and write, and the whole exchange
// follow ctx's deadline (or DefaultTimeout), and cancelling ctx closes the
// connection, so a relay that stalls can't hang the caller (#61).
func (s SMTP) Send(ctx context.Context, m Message) error {
	if strings.ContainsAny(m.To+m.Subject, "\r\n") {
		return fmt.Errorf("invalid header value")
	}
	var extra strings.Builder
	for k, v := range m.Headers {
		if strings.ContainsAny(k+v, "\r\n") || strings.ContainsAny(k, ": ") {
			return fmt.Errorf("invalid header %q", k)
		}
		extra.WriteString(k + ": " + v + "\r\n")
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	msg := "From: " + s.From + "\r\n" +
		"To: " + m.To + "\r\n" +
		"Subject: " + m.Subject + "\r\n" +
		"Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n" +
		extra.String() +
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		strings.ReplaceAll(m.Body, "\n", "\r\n")
	return s.deliver(ctx, addr, auth, extractAddr(s.From), m.To, []byte(msg))
}

// deliver is smtp.SendMail with a context: the same steps (EHLO, STARTTLS
// when offered, AUTH, MAIL, RCPT, DATA, QUIT) on a connection with deadlines.
func (s SMTP) deliver(ctx context.Context, addr string, auth smtp.Auth, from, to string, msg []byte) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultTimeout)
		defer cancel()
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		conn.Close()
		return err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: s.Host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

func extractAddr(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}
