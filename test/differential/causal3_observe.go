// SPDX-License-Identifier: Apache-2.0

package differential

import "fmt"

// a3Lane is the runtime position inside a planned hook or chain lane.
type a3Lane struct {
	spec       *causalLaneSpec
	pos        int
	activation int
}

// a3obs carries the observation state of amendment 3 (see the amendment: K1 to
// K8). It is created per observed trace; every rule it enforces runs on one
// trace alone.
type a3obs struct {
	m             *causalModel
	lanes         []*a3Lane
	byActor       map[string]*a3Lane
	cmdPos        map[string]int
	stage         map[string]int
	bundleDone    map[string]map[int]bool
	anchors       map[string]int
	requests      map[string]int
	rawLatest     map[string]any
	frameSeen     map[string]bool
	frameCfg      map[string]any
	wantApply     map[string]any
	hasWantApply  map[string]bool
	candidates    map[string][]any
	appliedCfgs   map[string][]any
	selfReq       map[string]bool
	rootPublished map[causalAddress]bool
	requestsAtCut []map[string]int
	errSeen       map[string]int
}

func (m *causalModel) newA3obs() *a3obs {
	o := &a3obs{m: m, byActor: make(map[string]*a3Lane), cmdPos: make(map[string]int), stage: make(map[string]int),
		bundleDone: make(map[string]map[int]bool), anchors: make(map[string]int), requests: make(map[string]int),
		rawLatest: make(map[string]any), frameSeen: make(map[string]bool), frameCfg: make(map[string]any),
		wantApply: make(map[string]any), hasWantApply: make(map[string]bool), candidates: make(map[string][]any),
		appliedCfgs: make(map[string][]any), selfReq: make(map[string]bool), rootPublished: make(map[causalAddress]bool),
		requestsAtCut: make([]map[string]int, len(m.cuts)), errSeen: make(map[string]int)}
	for _, spec := range m.a3.lanes {
		l := &a3Lane{spec: spec}
		o.lanes = append(o.lanes, l)
		o.byActor[spec.actor] = l
	}
	for fiber, config := range m.a3.mountConfig {
		o.rawLatest[fiber] = config
	}
	return o
}

func (o *a3obs) isLane(actor string) bool { return o.byActor[actor] != nil }

func (o *a3obs) lanePos(actor string) int {
	if l := o.byActor[actor]; l != nil {
		return l.pos
	}
	return 0
}

// lanesDone reports whether every need is met.
func (o *a3obs) lanesDone(needs []causalLaneNeed) bool {
	for _, n := range needs {
		if o.lanePos(n.lane) < n.pos {
			return false
		}
	}
	return true
}

// ready says whether a lane may take its next event: its requirements hold.
func (o *a3obs) ready(l *a3Lane, d a3deps) bool {
	if l.pos == 0 {
		if l.spec.chainFiber != "" && !o.bundleDone[l.spec.chainFiber][l.spec.chainCmd] {
			return false
		}
		if d.rootWaits(l.spec.waits) != nil {
			return false
		}
	}
	for _, r := range l.spec.reqs {
		if r.at == l.pos && o.lanePos(r.other) < r.n {
			return false
		}
	}
	return true
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

type a3deps struct {
	states      map[string]string
	activations map[string]int
	rootWaits   func([]causalWait) error
}

// laneFor finds the planned lane an event belongs to and consumes it. A nil lane
// with a nil error means the event is not a hook event. An event of a listener
// that the plan says must not run, or a planned event that is repeated, late or
// out of order, is a proved violation.
func (o *a3obs) laneFor(kind, key string, d a3deps) (*a3Lane, error) {
	specs := o.m.a3.laneItems[kind+"\x00"+key]
	if len(specs) == 0 {
		if o.m.a3.listenItems[kind+"\x00"+key] {
			return nil, fmt.Errorf("%s %q occurred although the plan says its listener does not run", kind, key)
		}
		return nil, nil
	}
	var candidates []*a3Lane
	for _, spec := range specs {
		l := o.byActor[spec.actor]
		if l.pos < len(spec.items) && spec.items[l.pos] == (causalItem{kind, key}) {
			candidates = append(candidates, l)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%s %q is repeated, late or out of its planned order", kind, key)
	}
	chosen := candidates[0]
	for _, c := range candidates {
		if o.ready(c, d) {
			chosen = c
			break
		}
	}
	l := chosen
	if l.pos == 0 {
		if l.spec.chainFiber != "" && !o.bundleDone[l.spec.chainFiber][l.spec.chainCmd] {
			return nil, fmt.Errorf("update listener %q started before its command's events", l.spec.actor)
		}
		if err := d.rootWaits(l.spec.waits); err != nil {
			return nil, err
		}
		if l.spec.chainFiber != "" {
			l.activation = d.activations[l.spec.chainFiber]
		}
	}
	for _, r := range l.spec.reqs {
		if r.at == l.pos && o.lanePos(r.other) < r.n {
			return nil, fmt.Errorf("%s %q of lane %q occurred before lane %q consumed %d events", kind, key, l.spec.actor, r.other, r.n)
		}
	}
	if f := l.spec.chainFiber; f != "" && (d.states[f] != "ACTIVE" || d.activations[f] != l.activation) {
		return nil, fmt.Errorf("hook continuation %q ran after its fiber %q left its activation", l.spec.actor, f)
	}
	l.pos++
	return l, nil
}

// bundleKinds is the ordered event kinds of a command's bundle (Vocabulary).
func bundleKinds(c *causalFiberCommand) []string {
	kinds := []string{"config"}
	if c.op == "update" {
		if !c.noSave {
			kinds = append(kinds, "commit")
		}
		if c.policy != "" {
			kinds = append(kinds, "update")
		}
	}
	return kinds
}

func (o *a3obs) request(f string) {
	o.anchors[f]++
	o.requests[f]++
}

// cmdEvent consumes one command-driven event of a new-shape fiber (K5, K6).
func (o *a3obs) cmdEvent(f, kind string, data map[string]any, effective any, hasEffective bool, d a3deps, applied bool) error {
	cmds := o.m.a3.fiberCmds[f]
	pos := o.cmdPos[f]
	if pos >= len(cmds) {
		return fmt.Errorf("extra command-driven %s event of %q", kind, f)
	}
	cmd := cmds[pos]
	kinds := bundleKinds(cmd)
	st := o.stage[f]
	if kinds[st] != kind {
		return fmt.Errorf("command bundle of %q: %s event where %s was expected", f, kind, kinds[st])
	}
	if st == 0 {
		state := d.states[f]
		loaded := state == "ACTIVE" || state == "UNLOADING" || state == "LOADING" && applied
		if !loaded || cmd.policy != "" && state != "ACTIVE" {
			return causalUnsupported("command on %q while its state %q is not provably loaded", f, state)
		}
		if err := d.rootWaits(cmd.waits); err != nil {
			return err
		}
	}
	switch kind {
	case "config":
		if cmd.op == "update" {
			o.rawLatest[f] = cmd.config // raw input is retained even under noSave
			o.candidates[f] = append(o.candidates[f], data["config"])
			if cmd.noSave && cmd.restart {
				o.request(f)
			}
		}
	case "commit":
		if !causalEqual(data["config"], cmd.config) {
			return fmt.Errorf("commit of %q carries a configuration other than its command's", f)
		}
		o.rawLatest[f] = cmd.config
		if cmd.restart {
			o.request(f)
		}
	case "update":
		if data["mode"] != cmd.policy {
			return fmt.Errorf("update hook event of %q has mode %v, plugin policy is %q", f, data["mode"], cmd.policy)
		}
		o.candidates[f] = append(o.candidates[f], data["config"])
		if cmd.policy == "unchanged" && (!hasEffective || !causalEqual(data["config"], effective)) {
			o.request(f)
			o.wantApply[f], o.hasWantApply[f] = data["config"], true
		}
	}
	o.stage[f]++
	if o.stage[f] == len(kinds) {
		if o.bundleDone[f] == nil {
			o.bundleDone[f] = make(map[int]bool)
		}
		o.bundleDone[f][pos] = true
		o.cmdPos[f]++
		o.stage[f] = 0
	}
	return nil
}

// checkCut enforces the settled-cut obligations of K6 for every new-shape fiber
// and completes the tasks (K6.6).
func (o *a3obs) checkCut(ci int, label string) error {
	spec := o.m.cuts[ci]
	for f, cmds := range o.m.a3.fiberCmds {
		due := 0
		for _, c := range cmds {
			if c.segment <= ci {
				due++
			}
		}
		if o.cmdPos[f] < due {
			return fmt.Errorf("checkpoint %q: a command of %q produced no complete event bundle", label, f)
		}
	}
	if !spec.snapshot {
		for f := range o.m.a3.newShape {
			if o.anchors[f] != 0 {
				return fmt.Errorf("checkpoint %q: update request of %q not served by a reload", label, f)
			}
		}
		if !o.lanesDone(spec.taskNeeds) {
			return fmt.Errorf("checkpoint %q: a task is not complete at a settled cut", label)
		}
	}
	snapshot := make(map[string]int, len(o.requests))
	for f, n := range o.requests {
		snapshot[f] = n
	}
	o.requestsAtCut[ci] = snapshot
	return nil
}

// finish checks that everything planned has happened by the end of the trace.
func (o *a3obs) finish() error {
	for _, l := range o.lanes {
		if l.pos != len(l.spec.items) {
			return fmt.Errorf("lane %q ended after %d of %d planned events", l.spec.actor, l.pos, len(l.spec.items))
		}
	}
	for f, cmds := range o.m.a3.fiberCmds {
		if o.cmdPos[f] != len(cmds) || o.stage[f] != 0 {
			return fmt.Errorf("commands of %q produced %d of %d event bundles", f, o.cmdPos[f], len(cmds))
		}
	}
	for address := range o.m.a3.rootProvides {
		if !o.rootPublished[address] {
			return fmt.Errorf("root provision %s/%s was never published", address.key, address.realm)
		}
	}
	return nil
}
