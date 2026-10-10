package format

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The naming rules. Names are identifiers rather than descriptions: one character
// set, reversible, and never carrying state that can change.

var (
	slugRE  = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	dateRE  = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(-\d+)?$`)
	convRE  = regexp.MustCompile(`^(\d{2})-([a-z0-9]+(-[a-z0-9]+)*)\.md$`)
	stampRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}Z$`)
)

// Layouts for the two forms time takes. Contents use the first, names the second,
// and they convert mechanically.
const (
	RFC3339UTC = "2006-01-02T15:04:05Z"
	StampForm  = "2006-01-02T15-04-05Z"
	DayForm    = "2006-01-02"
)

// MaxNameLength is the length beyond which a name stops being safe as one path
// component.
const MaxNameLength = 64

// ValidSlug reports whether s is a slug.
func ValidSlug(s string) bool { return slugRE.MatchString(s) && len(s) <= MaxNameLength }

// ValidStamp reports whether s is a name-form timestamp.
func ValidStamp(s string) bool { return stampRE.MatchString(s) }

// ValidDay reports whether s is a calendar date, which is the form an asset version
// and a retirement date share.
func ValidDay(s string) bool { return dateRE.MatchString(s) }

// ParseStamp reads a name-form timestamp.
func ParseStamp(s string) (time.Time, error) { return time.Parse(StampForm, s) }

// ParseUTC reads a timestamp written inside a file. The design says UTC, so anything
// carrying an offset is refused rather than converted: a record that says 02:10+08:00
// is not the same statement as one that says 18:10Z, even though the instant is.
func ParseUTC(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, err
	}
	if _, offset := t.Zone(); offset != 0 {
		return time.Time{}, fmt.Errorf("%q is not UTC", s)
	}
	return t.UTC(), nil
}

// StampToUTC converts the form used in a file name to the form used in its contents.
func StampToUTC(stamp string) (string, error) {
	t, err := ParseStamp(stamp)
	if err != nil {
		return "", fmt.Errorf("%q is not a timestamp: %w", stamp, err)
	}
	return t.Format(RFC3339UTC), nil
}

// UTCToStamp is the other direction.
func UTCToStamp(utc string) (string, error) {
	t, err := ParseUTC(utc)
	if err != nil {
		return "", err
	}
	return t.Format(StampForm), nil
}

// AssetTag is how a version is published.
func AssetTag(asset, version string) string { return asset + "@" + version }

// SplitAssetTag is its inverse.
func SplitAssetTag(tag string) (asset, version string, err error) {
	i := strings.LastIndex(tag, "@")
	if i <= 0 || i == len(tag)-1 {
		return "", "", fmt.Errorf("%q is not an asset tag", tag)
	}
	return tag[:i], tag[i+1:], nil
}

// ConventionFile parses a convention's file name and returns its number.
func ConventionFile(name string) (number int, ok bool) {
	m := convRE.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// LeaseFileName is how a held unit is named. The dot separates resource from unit
// because a slug never contains one, so the name stays unambiguous and reversible.
func LeaseFileName(resource, unit string) string {
	return resource + "." + unit + ".yaml"
}

// SplitLeaseFileName is its inverse.
func SplitLeaseFileName(name string) (resource, unit string, err error) {
	name = strings.TrimSuffix(name, ".yaml")
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return "", "", fmt.Errorf("%q is not a lease file name", name)
	}
	return name[:i], name[i+1:], nil
}

// HandoffFileName is the name-form timestamp of a stretch's start.
func HandoffFileName(started time.Time) string { return started.UTC().Format(StampForm) + ".md" }
