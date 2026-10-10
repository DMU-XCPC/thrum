package format

// The record types, one per format the design defines. Field names and requiredness
// follow the tables in the design document; a comment here names the rule it comes
// from only where the encoding is not obvious.

// Brief is doc/task/<job-id>/brief.md.
//
// Its body holds three sections — Goal, Scope, Acceptance criteria — and its status
// stores only what somebody has to decide: being blocked is read from the latest
// handoff, and having started is read from the handoff directory being non-empty.
type Brief struct {
	Job        string   `yaml:"job"`
	Status     string   `yaml:"status"`
	Branch     string   `yaml:"branch"`
	OwnedPaths []string `yaml:"owned_paths"`
	Owner      string   `yaml:"owner,omitempty"`
	DependsOn  []string `yaml:"depends_on,omitempty"`
	Keep       bool     `yaml:"keep,omitempty"`
}

// Brief statuses.
const (
	StatusActive    = "active"
	StatusDone      = "done"
	StatusAbandoned = "abandoned"
)

// Handoff statuses.
const (
	HandoffDone    = "done"
	HandoffPartial = "partial"
	HandoffBlocked = "blocked"
	HandoffAborted = "aborted"
)

// Artifact is one file a handoff leaves behind, hashed raw so that a collected
// artifact is a detectable absence rather than a silent one.
type Artifact struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
	Bytes  int64  `yaml:"bytes"`
}

// Handoff is handoff/<job-id>/<started-utc>.md.
//
// It covers one stretch of work: the actor that wrote it, the commits it began and
// ended at, and the baton in Next. Metrics keeps whatever numbers a stretch produced
// without fixing their names, because the design calls them "a map of numbers" and
// nothing reads them by name.
type Handoff struct {
	Job        string         `yaml:"job"`
	Agent      string         `yaml:"agent"`
	Branch     string         `yaml:"branch"`
	Base       string         `yaml:"base"`
	Head       string         `yaml:"head"`
	Status     string         `yaml:"status"`
	Started    string         `yaml:"started"`
	Ended      string         `yaml:"ended"`
	Next       string         `yaml:"next"`
	Worktrees  []string       `yaml:"worktrees,omitempty"`
	Supersedes string         `yaml:"supersedes,omitempty"`
	BlockedBy  []string       `yaml:"blocked_by,omitempty"`
	OwnedPaths []string       `yaml:"owned_paths,omitempty"`
	Metrics    map[string]any `yaml:"metrics,omitempty"`
	Artifacts  []Artifact     `yaml:"artifacts,omitempty"`
	Assets     []string       `yaml:"assets,omitempty"`
}

// Handoff body sections, in order.
var HandoffSections = []string{
	"What changed",
	"What was verified",
	"What remains",
	"Open questions",
}

// BriefSections are the brief's body sections, in order.
var BriefSections = []string{"Goal", "Scope", "Acceptance criteria"}

// ProducedBy names where an asset version came from.
type ProducedBy struct {
	Job    string `yaml:"job"`
	Commit string `yaml:"commit"`
}

// Manifest is doc/asset/<asset-name>/<version>.md.
//
// Sha256 and Bytes describe a version whose content is a single blob, or one held
// outside git. A version whose content is a directory leaves them out: the git
// object the tag names is its identity, and a checksum over a tar stream would only
// be stable for as long as nothing about the tar ever changed.
type Manifest struct {
	Asset      string      `yaml:"asset"`
	Version    string      `yaml:"version"`
	Location   string      `yaml:"location"`
	Ref        string      `yaml:"ref,omitempty"`
	SHA256     string      `yaml:"sha256,omitempty"`
	Bytes      int64       `yaml:"bytes,omitempty"`
	ProducedBy *ProducedBy `yaml:"produced_by,omitempty"`
	Source     string      `yaml:"source,omitempty"`
	License    string      `yaml:"license,omitempty"`
}

// Lease is var/lease/<resource>.<unit>.yaml. It is site-local, it always belongs to
// a job, and Unit is a string because YAML would otherwise read 02 as octal.
type Lease struct {
	Resource string `yaml:"resource"`
	Unit     string `yaml:"unit"`
	Occupant string `yaml:"occupant"`
	Job      string `yaml:"job"`
	Since    string `yaml:"since"`
	Expires  string `yaml:"expires"`
}

// Occupy is var/occupy/<job-id>.yaml: which actor the tooling is running on a job at
// this site, and since when. Written and cleared by the tooling, never by hand.
type Occupy struct {
	Occupant string `yaml:"occupant"`
	Since    string `yaml:"since"`
}

// Resource is one entry in a resource registry, doc/resource/<kind>.yaml. Only the
// first three fields are read by the tooling; a field it does not read is a field for
// the reader, which is why registries are decoded loosely and rewritten by node.
type Resource struct {
	Name      string `yaml:"name"`
	Purpose   string `yaml:"purpose"`
	Capacity  int    `yaml:"capacity,omitempty"`
	Ephemeral *bool  `yaml:"ephemeral,omitempty"`
}

// WorktreeKind is the one resource kind the design specifies.
const WorktreeKind = "worktree"
