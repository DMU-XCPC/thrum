// Package records holds the shapes of the files a workspace is made of. It is types
// and no rules: each package that owns a record reads and writes it.
package records

// Actor is a name's standing. Where it is now is the fabric's, not this file's.
type Actor struct {
	Actor      string `yaml:"actor" json:"actor"`
	Kind       string `yaml:"kind" json:"kind"`
	About      string `yaml:"about,omitempty" json:"about,omitempty"`
	Home       string `yaml:"home,omitempty" json:"home,omitempty"`
	Registered string `yaml:"registered,omitempty" json:"registered,omitempty"`
	Retired    string `yaml:"retired,omitempty" json:"retired,omitempty"`
}

// Site is one machine's entry in the roster. A site that has been invited but has
// not joined yet has no key.
type Site struct {
	Site  string `yaml:"site" json:"site"`
	Key   string `yaml:"key,omitempty" json:"key,omitempty"`
	Reach string `yaml:"reach,omitempty" json:"reach,omitempty"`
}

// Repo is one code repository the project has.
type Repo struct {
	Repo    string `yaml:"repo" json:"repo"`
	About   string `yaml:"about,omitempty" json:"about,omitempty"`
	Origin  string `yaml:"origin,omitempty" json:"origin,omitempty"`
	Retired string `yaml:"retired,omitempty" json:"retired,omitempty"`
}

// Worktree is a named area of work, bound to one repository.
type Worktree struct {
	Label     string `yaml:"label" json:"label"`
	Repo      string `yaml:"repo" json:"repo"`
	Purpose   string `yaml:"purpose" json:"purpose"`
	Ephemeral bool   `yaml:"ephemeral,omitempty" json:"ephemeral,omitempty"`
	Retired   string `yaml:"retired,omitempty" json:"retired,omitempty"`
}

// Brief states one job's intent.
type Brief struct {
	Job       string   `yaml:"job" json:"job"`
	Status    string   `yaml:"status" json:"status"`
	Repo      string   `yaml:"repo" json:"repo"`
	Branch    string   `yaml:"branch" json:"branch"`
	Owner     string   `yaml:"owner,omitempty" json:"owner,omitempty"`
	Touches   []string `yaml:"touches,omitempty" json:"touches,omitempty"`
	DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Keep      bool     `yaml:"keep,omitempty" json:"keep,omitempty"`
}

// Artifact is one file a handoff names, by raw checksum.
type Artifact struct {
	Path   string `yaml:"path" json:"path"`
	SHA256 string `yaml:"sha256" json:"sha256"`
	Bytes  int64  `yaml:"bytes" json:"bytes"`
}

// Handoff records one stretch of work.
type Handoff struct {
	Job        string         `yaml:"job" json:"job"`
	Repo       string         `yaml:"repo" json:"repo"`
	Agent      string         `yaml:"agent" json:"agent"`
	Branch     string         `yaml:"branch" json:"branch"`
	Base       string         `yaml:"base" json:"base"`
	Head       string         `yaml:"head" json:"head"`
	Status     string         `yaml:"status" json:"status"`
	Started    string         `yaml:"started" json:"started"`
	Ended      string         `yaml:"ended" json:"ended"`
	Next       string         `yaml:"next" json:"next"`
	Worktrees  []string       `yaml:"worktrees,omitempty" json:"worktrees,omitempty"`
	Supersedes string         `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
	BlockedBy  []string       `yaml:"blocked_by,omitempty" json:"blocked_by,omitempty"`
	Touches    []string       `yaml:"touches,omitempty" json:"touches,omitempty"`
	Metrics    map[string]any `yaml:"metrics,omitempty" json:"metrics,omitempty"`
	Artifacts  []Artifact     `yaml:"artifacts,omitempty" json:"artifacts,omitempty"`
	Assets     []string       `yaml:"assets,omitempty" json:"assets,omitempty"`
}

// ProducedBy names the job and commit a version came from.
type ProducedBy struct {
	Job    string `yaml:"job" json:"job"`
	Commit string `yaml:"commit" json:"commit"`
}

// Manifest anchors one version of one asset.
type Manifest struct {
	Asset      string      `yaml:"asset" json:"asset"`
	Version    string      `yaml:"version" json:"version"`
	Location   string      `yaml:"location" json:"location"`
	Ref        string      `yaml:"ref,omitempty" json:"ref,omitempty"`
	SHA256     string      `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	Bytes      int64       `yaml:"bytes,omitempty" json:"bytes,omitempty"`
	ProducedBy *ProducedBy `yaml:"produced_by,omitempty" json:"produced_by,omitempty"`
	Source     string      `yaml:"source,omitempty" json:"source,omitempty"`
	License    string      `yaml:"license,omitempty" json:"license,omitempty"`
}

// Place is what the fabric last knew about an actor. It is not a claim about working.
type Place struct {
	Site  string `yaml:"site" json:"site"`
	Heard string `yaml:"heard" json:"heard"`
}

// Places is the site-local table of last-known places.
type Places struct {
	Actors map[string]Place `yaml:"actors"`
}

// Attachment is one file carried beside a message.
type Attachment struct {
	Name string `yaml:"name" json:"name"`
	Size int64  `yaml:"size" json:"size"`
}

// Message is temporary work state: not a record, and never committed.
type Message struct {
	ID          string       `yaml:"id" json:"id"`
	From        string       `yaml:"from" json:"from"`
	To          string       `yaml:"to" json:"to"`
	Site        string       `yaml:"site" json:"site"`
	Sent        string       `yaml:"sent" json:"sent"`
	Subject     string       `yaml:"subject" json:"subject"`
	ReplyTo     string       `yaml:"reply_to,omitempty" json:"reply_to,omitempty"`
	Job         string       `yaml:"job,omitempty" json:"job,omitempty"`
	Attachments []Attachment `yaml:"attachments,omitempty" json:"attachments,omitempty"`
}
