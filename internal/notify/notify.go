// Package notify sends the optional new-order notifications (email, webhook)
// from a bounded background queue. Failures are logged without personal data
// and never affect the order, which is already saved.
package notify

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/omarchaouki/zaytona-go/internal/domain"
)

// Item is an order line.
type Item struct {
	Name      string          `json:"name"`
	Quantity  int             `json:"quantity"`
	UnitPrice decimal.Decimal `json:"unitPrice"`
}

// Order is what a notification carries.
type Order struct {
	Reference   string              `json:"reference"`
	Name        string              `json:"customerName"`
	PhoneE164   string              `json:"phone"`
	City        string              `json:"city"`
	Address     string              `json:"address,omitempty"`
	Note        string              `json:"note,omitempty"`
	Locale      string              `json:"locale"`
	Items       []Item              `json:"items"`
	Subtotal    decimal.Decimal     `json:"subtotal"`
	DeliveryFee decimal.NullDecimal `json:"deliveryFee"`
	Total       decimal.Decimal     `json:"total"`
	CreatedAt   time.Time           `json:"createdAt"`
}

// Targets returns where to send right now (read from Settings on every order,
// so an admin change applies immediately).
type Targets func(ctx context.Context) domain.NotificationSettings

// Config is the env part of the notifications.
type Config struct {
	SMTPURL       string // smtp(s)://user:pass@host:port
	EmailFrom     string
	WebhookURL    string // ORDER_WEBHOOK_URL, used when Settings has none
	WebhookSecret []byte // HMAC key for X-Zaytona-Signature
	Production    bool
}

// Notifier runs a small worker pool over a bounded queue.
type Notifier struct {
	cfg     Config
	targets Targets
	log     *slog.Logger
	queue   chan Order
	wg      sync.WaitGroup
	webhook *Webhook
	email   func(ctx context.Context, to string, o Order) error
}

// New starts the workers. Close drains the queue on shutdown.
func New(cfg Config, targets Targets, log *slog.Logger) *Notifier {
	n := &Notifier{
		cfg:     cfg,
		targets: targets,
		log:     log,
		queue:   make(chan Order, 100),
		webhook: NewWebhook(cfg.WebhookSecret, !cfg.Production),
	}
	n.email = n.sendEmail
	for range 2 {
		n.wg.Go(n.work)
	}
	return n
}

// Enqueue never blocks: when the queue is full the notification is dropped
// (and logged); the order itself is safe in the database.
func (n *Notifier) Enqueue(o Order) {
	select {
	case n.queue <- o:
	default:
		n.log.Warn("notify queue full, notification dropped", "reference", o.Reference)
	}
}

// Close stops accepting work and waits for the queue to drain (bounded by ctx).
func (n *Notifier) Close(ctx context.Context) {
	close(n.queue)
	done := make(chan struct{})
	go func() { n.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		n.log.Warn("notify: shutdown before the queue drained")
	}
}

func (n *Notifier) work() {
	for o := range n.queue {
		n.send(o)
	}
}

func (n *Notifier) send(o Order) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t := n.targets(ctx)
	if t.Email != "" && n.cfg.SMTPURL != "" && n.cfg.EmailFrom != "" {
		if err := n.email(ctx, t.Email, o); err != nil {
			n.log.Error("notify email failed", "reference", o.Reference, "err", err)
		}
	}
	hook := t.WebhookURL
	if hook == "" {
		hook = n.cfg.WebhookURL
	}
	if hook != "" {
		if err := n.webhook.Post(ctx, hook, o); err != nil {
			n.log.Error("notify webhook failed", "reference", o.Reference, "err", err)
		}
	}
}

// Text is the email body (French, for the owner).
func Text(o Order) string {
	fee := "à confirmer"
	if o.DeliveryFee.Valid {
		fee = domain.FormatMAD(o.DeliveryFee.Decimal, "fr")
	}
	lines := []string{
		"Nouvelle commande " + o.Reference,
		"",
		"Client : " + o.Name,
		"Téléphone : " + domain.FormatPhoneDisplay(o.PhoneE164),
		"Ville : " + o.City,
	}
	if o.Address != "" {
		lines = append(lines, "Adresse : "+o.Address)
	}
	if o.Note != "" {
		lines = append(lines, "Note : "+o.Note)
	}
	lines = append(lines, "")
	for _, it := range o.Items {
		lines = append(lines, "- "+strconv.Itoa(it.Quantity)+" × "+it.Name)
	}
	lines = append(lines, "",
		"Sous-total : "+domain.FormatMAD(o.Subtotal, "fr"),
		"Livraison : "+fee,
		"Total : "+domain.FormatMAD(o.Total, "fr"),
		"Langue du client : "+o.Locale,
	)
	return strings.Join(lines, "\n")
}
