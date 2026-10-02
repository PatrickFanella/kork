// Package mail sends account emails over SMTP, or logs them in development.
package mail

import (
	"context"
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

func (s SMTP) Send(_ context.Context, m Message) error {
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
	return smtp.SendMail(addr, auth, extractAddr(s.From), []string{m.To}, []byte(msg))
}

func extractAddr(from string) string {
	if i := strings.LastIndex(from, "<"); i >= 0 {
		return strings.TrimSuffix(from[i+1:], ">")
	}
	return from
}
