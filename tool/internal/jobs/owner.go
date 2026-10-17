package jobs

import (
	"fmt"

	"github.com/multiagentworkspace/thrum/tool/internal/layout"
	"github.com/multiagentworkspace/thrum/tool/internal/line"
)

// Claim gives an unowned job an owner. Taking ownership from somebody is a
// reassignment, not a claim.
func Claim(l layout.Layout, jobID, actor string) error {
	b, body, ok, err := ReadBrief(l, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no job named %q", jobID)
	}
	if b.Owner != "" {
		return fmt.Errorf("job %q is owned by %s; taking it over is a reassignment", jobID, b.Owner)
	}
	b.Owner = actor
	if err := writeBriefAt(l.Brief(jobID), b, body); err != nil {
		return err
	}
	return line.Commit(l.Root, []string{l.Brief(jobID)},
		fmt.Sprintf("brief %s: claimed by %s", jobID, actor))
}

// Reassign changes an owner.
func Reassign(l layout.Layout, jobID, to string) error {
	b, body, ok, err := ReadBrief(l, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no job named %q", jobID)
	}
	b.Owner = to
	if err := writeBriefAt(l.Brief(jobID), b, body); err != nil {
		return err
	}
	return line.Commit(l.Root, []string{l.Brief(jobID)},
		fmt.Sprintf("brief %s: owner set to %s", jobID, to))
}

// Relinquish clears an owner, which makes the job claimable by whoever takes it up.
func Relinquish(l layout.Layout, jobID string) error {
	b, body, ok, err := ReadBrief(l, jobID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("no job named %q", jobID)
	}
	if b.Owner == "" {
		return fmt.Errorf("job %q has no owner to give up", jobID)
	}
	b.Owner = ""
	if err := writeBriefAt(l.Brief(jobID), b, body); err != nil {
		return err
	}
	return line.Commit(l.Root, []string{l.Brief(jobID)},
		fmt.Sprintf("brief %s: owner given up", jobID))
}
