package main

import (
	"strings"
	"sync"
)

// subnetState is the committed subnet. Its session increases whenever the
// canonical subnet changes, so results from a round started on an older
// subnet are rejected even if the user later returns to the same subnet.
type subnetState struct {
	base      [4]byte
	canonical string
	session   uint64
}

func (s subnetState) valid() bool { return s.canonical != "" }

// commitResult describes the effect of committing subnet text.
type commitResult struct {
	Base      [4]byte
	Canonical string
	Session   uint64
	Changed   bool
	Err       error
}

// subnetModel owns the committed subnet and the host map. The Windows layer
// reads it for every scan, monitor round, hover status and context-menu action;
// the editable ComboBox text is never used as the source of truth.
type subnetModel struct {
	mu    sync.Mutex
	state subnetState
	hosts [hostLast + 1]hostRecord
}

func newSubnetModel() *subnetModel { return &subnetModel{} }

// Commit parses text and makes it authoritative. A canonical change clears the
// host map and increments the session; committing the same canonical subnet is
// a no-op that keeps the map and the session. Invalid text leaves everything
// unchanged and reports the parse error.
func (m *subnetModel) Commit(text string) commitResult {
	base, canonical, err := parseSubnet(text)

	m.mu.Lock()
	defer m.mu.Unlock()

	if err != nil {
		return commitResult{
			Base:      m.state.base,
			Canonical: m.state.canonical,
			Session:   m.state.session,
			Err:       err,
		}
	}
	if m.state.valid() && m.state.canonical == canonical {
		return commitResult{
			Base:      m.state.base,
			Canonical: canonical,
			Session:   m.state.session,
		}
	}

	m.state = subnetState{base: base, canonical: canonical, session: m.state.session + 1}
	m.hosts = [hostLast + 1]hostRecord{}
	return commitResult{
		Base:      base,
		Canonical: canonical,
		Session:   m.state.session,
		Changed:   true,
	}
}

// Current returns the committed subnet and the session rounds must carry.
func (m *subnetModel) Current() (base [4]byte, canonical string, session uint64, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.base, m.state.canonical, m.state.session, m.state.valid()
}

// Session returns the current scan session without exposing the subnet.
func (m *subnetModel) Session() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.session
}

// CanonicalText returns the committed CIDR, or "" when nothing is committed.
func (m *subnetModel) CanonicalText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state.canonical
}

// Snapshot copies the host map for painting.
func (m *subnetModel) Snapshot() [hostLast + 1]hostRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts
}

// Host returns the current record of one address.
func (m *subnetModel) Host(i int) hostRecord {
	if i < hostFirst || i > hostLast {
		return hostRecord{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hosts[i]
}

// Targets returns the addresses a round must probe: every usable host for a
// discovery sweep, only promoted hosts for an active round.
func (m *subnetModel) Targets(discovery bool) []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return targets(discovery, m.hosts[:])
}

// Observe applies a probe result only when the round belongs to the current
// session and, for an active round, the host is still promoted. A result from a
// cancelled or replaced round is dropped.
func (m *subnetModel) Observe(session uint64, i int, discovery, responded bool, rttMS uint32) bool {
	if i < hostFirst || i > hostLast {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if session != m.state.session || !m.state.valid() {
		return false
	}
	if !discovery && !m.hosts[i].promoted {
		return false
	}
	m.hosts[i].observe(discovery, responded, rttMS)
	return true
}

// subnetController keeps the editable ComboBox text separate from the
// committed subnet. Pending text is input; only CommitPending and Select make
// it authoritative.
type subnetController struct {
	model       *subnetModel
	pending     string
	dropText    string
	dropSession uint64
}

func newSubnetController() *subnetController {
	return &subnetController{model: newSubnetModel()}
}

// Model exposes the committed state to the probe and paint paths.
func (c *subnetController) Model() *subnetModel { return c.model }

// Text is the pending edit-field text.
func (c *subnetController) Text() string { return c.pending }

// SetText records the pending edit-field text without committing it.
func (c *subnetController) SetText(text string) { c.pending = text }

// CommitPending commits the pending text for an explicit action: Scan, Monitor
// mode or the context menu. On success the pending text becomes the canonical
// CIDR so the edit field always shows what will be scanned.
func (c *subnetController) CommitPending() commitResult {
	r := c.model.Commit(c.pending)
	if r.Err == nil {
		c.pending = r.Canonical
	}
	return r
}

// CommitPendingIfValid commits only text that already parses. The 15 s sweep
// uses it so a manually typed CIDR takes effect while monitoring continues;
// partial text is left for the user to finish.
func (c *subnetController) CommitPendingIfValid() (commitResult, bool) {
	if _, _, err := parseSubnet(c.pending); err != nil {
		return commitResult{}, false
	}
	return c.CommitPending(), true
}

// Select commits a drop-down entry by its CIDR. The CIDR is captured from the
// list, not parsed from the ComboBox text.
func (c *subnetController) Select(cidr string) commitResult {
	previous := c.pending
	c.pending = cidr
	r := c.CommitPending()
	if r.Err != nil {
		c.pending = previous
	}
	return r
}

// BeginDrop records the edit text and session when the drop-down opens.
func (c *subnetController) BeginDrop(text string) {
	c.dropText = text
	c.dropSession = c.model.Session()
}

// CancelDrop handles a drop-down that closed without a usable selection. A
// selection committed while the list was open wins over the pre-drop text, so
// cancelling after choosing cannot restore the previous subnet. Otherwise the
// pre-drop text, including a manually typed CIDR, returns. The committed
// subnet is never changed by a cancellation.
func (c *subnetController) CancelDrop() string {
	if c.model.Session() != c.dropSession {
		c.pending = c.model.CanonicalText()
		return c.pending
	}
	c.pending = c.dropText
	return c.pending
}

// Refresh keeps pending input and never changes the committed subnet or the
// host map. An empty edit field falls back to the committed CIDR.
func (c *subnetController) Refresh() string {
	if strings.TrimSpace(c.pending) == "" {
		c.pending = c.model.CanonicalText()
	}
	return c.pending
}

// resolveSubnetInput accepts a typed CIDR or a ComboBox label and returns the
// CIDR to commit. A selected entry writes its label into the editable field, so
// the label must map back to the CIDR that the entry stands for.
func resolveSubnetInput(text string, candidates []networkCandidate) string {
	trimmed := strings.TrimSpace(text)
	if _, _, err := parseSubnet(trimmed); err == nil {
		return trimmed
	}
	for _, c := range candidates {
		if trimmed == c.Label() {
			return c.CIDR
		}
	}
	return text
}
