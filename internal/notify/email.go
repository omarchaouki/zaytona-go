package notify

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	mail "github.com/wneessen/go-mail"
)

// sendEmail sends the owner email through SMTP_URL:
// smtps://user:pass@host:465 (implicit TLS) or smtp://user:pass@host:587 (STARTTLS required).
func (n *Notifier) sendEmail(ctx context.Context, to string, o Order) error {
	u, err := url.Parse(n.cfg.SMTPURL)
	if err != nil || u.Hostname() == "" {
		return fmt.Errorf("SMTP_URL is not a valid smtp(s):// URL")
	}
	opts := []mail.Option{mail.WithTimeout(10 * time.Second), mail.WithTLSPolicy(mail.TLSMandatory)}
	if p := u.Port(); p != "" {
		port, err := strconv.Atoi(p)
		if err != nil {
			return fmt.Errorf("SMTP_URL port: %w", err)
		}
		opts = append(opts, mail.WithPort(port))
	}
	if u.Scheme == "smtps" {
		opts = append(opts, mail.WithSSL())
	}
	if u.User != nil {
		pass, _ := u.User.Password()
		opts = append(opts, mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover), mail.WithUsername(u.User.Username()), mail.WithPassword(pass))
	}
	client, err := mail.NewClient(u.Hostname(), opts...)
	if err != nil {
		return err
	}
	msg := mail.NewMsg()
	if err := msg.From(n.cfg.EmailFrom); err != nil {
		return err
	}
	if err := msg.To(to); err != nil {
		return err
	}
	msg.Subject(fmt.Sprintf("Nouvelle commande %s · %s (%s)", o.Reference, o.Name, o.City))
	msg.SetBodyString(mail.TypeTextPlain, Text(o))
	return client.DialAndSendWithContext(ctx, msg)
}
