package view

import "strconv"

// ReviewsData is the landing reviews section (nil when the owner hides it).
type ReviewsData struct {
	// Summary of approved, non-example reviews; nil until there is one.
	Summary *ReviewSummary
	List    ReviewList
	Form    ReviewFormData
}

// ReviewSummary is the average, the count and the per-star bars.
type ReviewSummary struct {
	Average     float64
	AverageText string
	CountText   string
	Bars        []ReviewBar
	// AllReal: no example review is listed (the "real customers" note shows).
	AllReal bool
}

// ReviewBar is one line of the distribution; it filters the list.
type ReviewBar struct {
	Stars  int
	Count  int64
	Pct    int
	Href   string // no-JS filter link
	Active bool
}

// ReviewList is a page of cards (also the "load more" fragment).
type ReviewList struct {
	Items    []ReviewCard
	Stars    int // active filter (0: all)
	NextPage int // 0: no more
	MoreHref string
	AllHref  string
	// FragmentBase is /{locale}/reviews (JS asks it for more pages).
	FragmentBase string
}

// ReviewCard is one published review.
type ReviewCard struct {
	ID, Name, City, Comment, Lang, Day, Reply string
	Rating                                    int
	Verified, Demo                            bool
	Photo                                     Image
}

// ReviewFormData fills the review form (values and errors after a failed post).
type ReviewFormData struct {
	Token     string
	Values    map[string]string
	Errors    map[string]string
	FormError string
	Sent      bool
	MaxMB     string
}

func (f ReviewFormData) v(name string) string { return f.Values[name] }

// barWidth maps a percentage to a fixed width class (no style attributes, CSP).
func barWidth(pct int) string {
	return barWidths[min(max((pct+2)/5, 0), 20)]
}

// Literal class names so Tailwind generates them.
var barWidths = [21]string{
	"w-[0%]", "w-[5%]", "w-[10%]", "w-[15%]", "w-[20%]", "w-[25%]", "w-[30%]",
	"w-[35%]", "w-[40%]", "w-[45%]", "w-[50%]", "w-[55%]", "w-[60%]", "w-[65%]",
	"w-[70%]", "w-[75%]", "w-[80%]", "w-[85%]", "w-[90%]", "w-[95%]", "w-[100%]",
}

func itoa(n int) string { return strconv.Itoa(n) }

func i64(n int64) string { return strconv.FormatInt(n, 10) }
