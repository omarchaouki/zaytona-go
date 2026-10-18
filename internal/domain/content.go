package domain

// Editable page texts (content_blocks), one JSON object per language. Empty
// fields fall back to the translation files, so the page never shows a blank.

// HeroContent is Admin > Content > Hero.
type HeroContent struct {
	Eyebrow  string `json:"eyebrow"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

// StoryContent is Admin > Content > Story (body: paragraphs separated by blank lines).
type StoryContent struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// PoliciesContent are the short policy texts of the "why us" cards.
type PoliciesContent struct {
	COD      string `json:"cod"`
	Delivery string `json:"delivery"`
	Returns  string `json:"returns"`
	Privacy  string `json:"privacy"`
}

// PageContent is a full policy page (privacy, terms): sanitized HTML on render.
// Reviewed stays false until the client's lawyer signs off.
type PageContent struct {
	Title    string `json:"title"`
	HTML     string `json:"html"`
	Reviewed bool   `json:"reviewed"`
}
