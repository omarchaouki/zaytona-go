package view

import (
	"strconv"
	"strings"

	"github.com/omarchaouki/zaytona-go/internal/domain"
)

// Action styles of the public site: one filled olive button for the action
// that matters, text links for the rest. Flat, round, a small press.
const (
	actPrimary     = "inline-flex " + actFill
	actFill        = "btn-press bg-olive text-white hover:bg-olive-hover" // no display: for "hidden sm:inline-flex"
	actOnField     = "btn-press inline-flex bg-limewash text-ink hover:bg-white"
	actSecondary   = "btn-press inline-flex bg-ink text-limewash hover:bg-olive-950"
	textLink       = "tap inline-flex w-fit items-center gap-2 font-medium underline decoration-olive decoration-2 underline-offset-[6px] transition-colors hover:text-olive"
	textLinkOnDark = "tap inline-flex w-fit items-center gap-2 font-medium text-limewash underline decoration-oil-bright decoration-2 underline-offset-[6px] transition-colors hover:text-oil-bright"
)

// btn returns the button classes of the reference (cva buttonVariants base).
// The reference merged overrides with tailwind-merge; plain CSS cannot (the
// later rule in the stylesheet wins), so overrides are arguments here:
// a "px-*" replaces the size padding and "whitespace-normal" replaces nowrap.
// It sets no display: callers add "inline-flex" or "hidden sm:inline-flex".
func btn(size string, overrides ...string) string {
	classes := []string{"min-h-11 items-center justify-center gap-2 rounded-full font-semibold transition-[background-color,color,box-shadow,transform] duration-150 select-none active:translate-y-px [&_svg]:size-5 [&_svg]:shrink-0"}
	padX, wrap := "px-5", "whitespace-nowrap"
	text, padY := "text-[0.9375rem]", "py-2.5"
	if size == "lg" {
		padX, text, padY = "px-7", "text-base", "py-3.5"
	}
	for _, o := range overrides {
		switch {
		case strings.HasPrefix(o, "px-"):
			padX = o
		case o == "whitespace-normal":
			wrap = o
		}
	}
	return strings.Join(append(classes, padX, padY, text, wrap), " ")
}

func yearString(y int) string { return strconv.Itoa(y) }

func phoneDisplay(e164 string) string { return domain.FormatPhoneDisplay(e164) }
