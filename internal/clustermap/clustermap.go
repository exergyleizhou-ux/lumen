// Package clustermap provides a cluster topology map: node registration,
// health heartbeats, leader election (bully algorithm), gossip protocol
// simulation, and partition detection.
package clustermap

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"sort"
	"sync"
	"time"
)

// NodeID uniquely identifies a node in the cluster.
type NodeID string

// NewNodeID generates a random node ID.
func NewNodeID() NodeID {
	b := make([]byte, 8)
	rand.Read(b)
	return NodeID(hex.EncodeToString(b))
}

// NodeState represents the current state of a node.
type NodeState int

const (
	// StateUnknown means the node state is unknown.
	StateUnknown NodeState = iota
	// StateAlive means the node is healthy and responding.
	StateAlive
	// StateSuspected means the node may be down.
	StateSuspected
	// StateDead means the node is confirmed down.
	StateDead
	// StateLeft means the node gracefully left the cluster.
	StateLeft
)

var stateStrings = map[NodeState]string{
	StateUnknown:   "unknown",
	StateAlive:     "alive",
	StateSuspected: "suspected",
	StateDead:      "dead",
	StateLeft:      "left",
}

func (s NodeState) String() string {
	if str, ok := stateStrings[s]; ok {
		return str
	}
	return "unknown"
}

// Node represents a cluster member.
type Node struct {
	ID        NodeID
	Address   string
	Port      int
	State     NodeState
	Role      string
	Labels    map[string]string
	StartedAt time.Time
	LastSeen  time.Time
	Version   string
}

// NewNode creates a new node with the given address and port.
func NewNode(address string, port int) *Node {
	now := time.Now()
	return &Node{
		ID:        NewNodeID(),
		Address:   address,
		Port:      port,
		State:     StateAlive,
		Role:      "member",
		Labels:    make(map[string]string),
		StartedAt: now,
		LastSeen:  now,
		Version:   "1.0.0",
	}
}

// ---- Cluster Topology Map ----

// Topology holds the complete cluster map.
type Topology struct {
	mu        sync.RWMutex
	nodes     map[NodeID]*Node
	self      NodeID
	version   int64
	createdAt time.Time
	updatedAt time.Time
}

// NewTopology creates a new topology map.
func NewTopology(selfID NodeID) *Topology {
	now := time.Now()
	return &Topology{
		nodes:     make(map[NodeID]*Node),
		self:      selfID,
		createdAt: now,
		updatedAt: now,
	}
}

// AddNode registers a node in the topology.
func (t *Topology) AddNode(node *Node) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.nodes[node.ID] = node
	t.version++
	t.updatedAt = time.Now()
}

// RemoveNode removes a node from the topology.
func (t *Topology) RemoveNode(id NodeID) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.nodes, id)
	t.version++
	t.updatedAt = time.Now()
}

// GetNode returns a node by ID.
func (t *Topology) GetNode(id NodeID) (*Node, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	n, ok := t.nodes[id]
	return n, ok
}

// ListNodes returns all nodes in the topology.
func (t *Topology) ListNodes() []*Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	nodes := make([]*Node, 0, len(t.nodes))
	for _, n := range t.nodes {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})
	return nodes
}

// NodeCount returns the number of nodes.
func (t *Topology) NodeCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.nodes)
}

// Version returns the current topology version.
func (t *Topology) Version() int64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.version
}

// AliveNodes returns only nodes in StateAlive.
func (t *Topology) AliveNodes() []*Node {
	t.mu.RLock()
	defer t.mu.RUnlock()
	var alive []*Node
	for _, n := range t.nodes {
		if n.State == StateAlive {
			alive = append(alive, n)
		}
	}
	return alive
}

// UpdateNodeState changes a node's state.
func (t *Topology) UpdateNodeState(id NodeID, state NodeState) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	n, ok := t.nodes[id]
	if !ok {
		return false
	}
	n.State = state
	n.LastSeen = time.Now()
	t.version++
	t.updatedAt = time.Now()
	return true
}

// ---- Health / Heartbeats ----

// Heartbeat represents a health check pulse.
type Heartbeat struct {
	NodeID    NodeID
	Timestamp time.Time
	Sequence  uint64
	Load      float64 // CPU load 0.0-1.0
}

// HeartbeatMonitor tracks heartbeats from cluster nodes.
type HeartbeatMonitor struct {
	mu         sync.RWMutex
	heartbeats map[NodeID]*Heartbeat
	timeout    time.Duration
	onTimeout  func(NodeID)
	onRecover  func(NodeID)
	stopCh     chan struct{}
}

// NewHeartbeatMonitor creates a heartbeat monitor.
func NewHeartbeatMonitor(timeout time.Duration) *HeartbeatMonitor {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &HeartbeatMonitor{
		heartbeats: make(map[NodeID]*Heartbeat),
		timeout:    timeout,
		stopCh:     make(chan struct{}),
	}
}

// OnTimeout registers a callback for when a node times out.
func (hm *HeartbeatMonitor) OnTimeout(fn func(NodeID)) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.onTimeout = fn
}

// OnRecover registers a callback for when a node recovers.
func (hm *HeartbeatMonitor) OnRecover(fn func(NodeID)) {
	hm.mu.Lock()
	defer hm.mu.Unlock()
	hm.onRecover = fn
}

// ReceiveHeartbeat records a heartbeat from a node.
func (hm *HeartbeatMonitor) ReceiveHeartbeat(hb *Heartbeat) {
	hm.mu.Lock()
	defer hm.mu.Unlock()

	prev, existed := hm.heartbeats[hb.NodeID]
	hm.heartbeats[hb.NodeID] = hb

	if existed && prev.Sequence > 0 && prev.Sequence+1 < hb.Sequence {
		// Gap detected, but we still accept
	}
}

// CheckTimeouts checks for nodes that have not sent heartbeats within the timeout.
func (hm *HeartbeatMonitor) CheckTimeouts() []NodeID {
	hm.mu.RLock()
	defer hm.mu.RUnlock()

	now := time.Now()
	var timedOut []NodeID
	for id, hb := range hm.heartbeats {
		if now.Sub(hb.Timestamp) > hm.timeout {
			timedOut = append(timedOut, id)
		}
	}
	return timedOut
}

// Start begins periodic timeout checking.
func (hm *HeartbeatMonitor) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				timedOut := hm.CheckTimeouts()
				for _, id := range timedOut {
					if hm.onTimeout != nil {
						hm.onTimeout(id)
					}
				}
			case <-hm.stopCh:
				return
			}
		}
	}()
}

// Stop halts the monitor.
func (hm *HeartbeatMonitor) Stop() {
	close(hm.stopCh)
}

// LastHeartbeat returns the last heartbeat for a node.
func (hm *HeartbeatMonitor) LastHeartbeat(id NodeID) (*Heartbeat, bool) {
	hm.mu.RLock()
	defer hm.mu.RUnlock()
	hb, ok := hm.heartbeats[id]
	return hb, ok
}

// ---- Leader Election (Bully Algorithm) ----

// BullyElection implements the bully leader election algorithm.
type BullyElection struct {
	mu         sync.RWMutex
	nodes      []NodeID
	self       NodeID
	leader     NodeID
	epoch      int64
	electionCh chan NodeID
}

// NewBullyElection creates a bully election instance.
func NewBullyElection(self NodeID, peerIDs []NodeID) *BullyElection {
	allNodes := append([]NodeID{self}, peerIDs...)
	sort.Slice(allNodes, func(i, j int) bool {
		return allNodes[i] < allNodes[j]
	})
	return &BullyElection{
		nodes:      allNodes,
		self:       self,
		electionCh: make(chan NodeID, 10),
	}
}

// Leader returns the current leader.
func (be *BullyElection) Leader() NodeID {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return be.leader
}

// Epoch returns the current election epoch.
func (be *BullyElection) Epoch() int64 {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return be.epoch
}

// StartElection initiates a leader election.
func (be *BullyElection) StartElection() NodeID {
	be.mu.Lock()
	defer be.mu.Unlock()

	be.epoch++
	// Bully algorithm: the node with the highest ID becomes leader
	// In our sorted list, the last node is the highest
	if len(be.nodes) > 0 {
		be.leader = be.nodes[len(be.nodes)-1]
	}
	be.electionCh <- be.leader
	return be.leader
}

// ElectionChannel returns a channel that receives leader updates.
func (be *BullyElection) ElectionChannel() <-chan NodeID {
	return be.electionCh
}

// IsLeader returns true if this node is the current leader.
func (be *BullyElection) IsLeader() bool {
	be.mu.RLock()
	defer be.mu.RUnlock()
	return be.leader == be.self
}

// AddNode adds a peer to the election set.
func (be *BullyElection) AddNode(id NodeID) {
	be.mu.Lock()
	defer be.mu.Unlock()
	be.nodes = append(be.nodes, id)
	sort.Slice(be.nodes, func(i, j int) bool {
		return be.nodes[i] < be.nodes[j]
	})
}

// RemoveNode removes a peer from the election set.
func (be *BullyElection) RemoveNode(id NodeID) {
	be.mu.Lock()
	defer be.mu.Unlock()
	for i, n := range be.nodes {
		if n == id {
			be.nodes = append(be.nodes[:i], be.nodes[i+1:]...)
			break
		}
	}
	// If the leader was removed, trigger a new election
	if be.leader == id {
		be.leader = NodeID("")
		if len(be.nodes) > 0 {
			be.leader = be.nodes[len(be.nodes)-1]
			be.epoch++
		}
	}
}

// Nodes returns all node IDs in the election.
func (be *BullyElection) Nodes() []NodeID {
	be.mu.RLock()
	defer be.mu.RUnlock()
	result := make([]NodeID, len(be.nodes))
	copy(result, be.nodes)
	return result
}

// ---- Gossip Protocol ----

// GossipMessage represents a message in the gossip protocol.
type GossipMessage struct {
	From      NodeID
	Data      map[string]string
	TTL       int
	Timestamp time.Time
	ID        string
}

// GossipProtocol simulates a gossip-style message dissemination protocol.
type GossipProtocol struct {
	mu        sync.RWMutex
	nodes     map[NodeID]*GossipState
	fanout    int
	seenMsgs  map[string]time.Time
	msgLog    []GossipMessage
	onMessage func(GossipMessage)
}

// GossipState tracks per-node gossip state.
type GossipState struct {
	NodeID     NodeID
	LastGossip time.Time
	MsgCount   int
	Alive      bool
}

// NewGossipProtocol creates a gossip protocol instance.
func NewGossipProtocol(fanout int) *GossipProtocol {
	if fanout < 1 {
		fanout = 3
	}
	return &GossipProtocol{
		nodes:    make(map[NodeID]*GossipState),
		fanout:   fanout,
		seenMsgs: make(map[string]time.Time),
	}
}

// AddNode adds a node to the gossip cluster.
func (gp *GossipProtocol) AddNode(id NodeID) {
	gp.mu.Lock()
	defer gp.mu.Unlock()
	gp.nodes[id] = &GossipState{NodeID: id, Alive: true}
}

// RemoveNode removes a node.
func (gp *GossipProtocol) RemoveNode(id NodeID) {
	gp.mu.Lock()
	defer gp.mu.Unlock()
	delete(gp.nodes, id)
}

// OnMessage registers a callback for received gossip messages.
func (gp *GossipProtocol) OnMessage(fn func(GossipMessage)) {
	gp.mu.Lock()
	defer gp.mu.Unlock()
	gp.onMessage = fn
}

// Gossip sends a message through the gossip protocol.
// It randomly selects fanout nodes and "sends" the message.
func (gp *GossipProtocol) Gossip(from NodeID, data map[string]string, ttl int) {
	gp.mu.Lock()
	defer gp.mu.Unlock()

	msg := GossipMessage{
		From:      from,
		Data:      data,
		TTL:       ttl,
		Timestamp: time.Now(),
		ID:        generateMsgID(),
	}

	if _, seen := gp.seenMsgs[msg.ID]; seen {
		return
	}
	gp.seenMsgs[msg.ID] = time.Now()
	gp.msgLog = append(gp.msgLog, msg)

	if state, ok := gp.nodes[from]; ok {
		state.LastGossip = time.Now()
		state.MsgCount++
	}

	// Select fanout random alive nodes
	targets := gp.selectTargets(from, gp.fanout)
	for _, target := range targets {
		if state, ok := gp.nodes[target]; ok {
			state.LastGossip = time.Now()
			state.MsgCount++
		}
	}

	if gp.onMessage != nil {
		gp.onMessage(msg)
	}
}

func (gp *GossipProtocol) selectTargets(from NodeID, count int) []NodeID {
	var alive []NodeID
	for id, state := range gp.nodes {
		if id != from && state.Alive {
			alive = append(alive, id)
		}
	}
	if len(alive) <= count {
		return alive
	}

	// Shuffle and take first count
	shuffled := make([]NodeID, len(alive))
	copy(shuffled, alive)
	for i := range shuffled {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(len(shuffled))))
		shuffled[i], shuffled[j.Int64()] = shuffled[j.Int64()], shuffled[i]
	}
	return shuffled[:count]
}

// MessageLog returns all messages seen.
func (gp *GossipProtocol) MessageLog() []GossipMessage {
	gp.mu.RLock()
	defer gp.mu.RUnlock()
	result := make([]GossipMessage, len(gp.msgLog))
	copy(result, gp.msgLog)
	return result
}

// NodeCount returns the number of nodes in the gossip cluster.
func (gp *GossipProtocol) NodeCount() int {
	gp.mu.RLock()
	defer gp.mu.RUnlock()
	return len(gp.nodes)
}

func generateMsgID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- Partition Detection ----

// PartitionDetector monitors for network partitions.
type PartitionDetector struct {
	mu            sync.RWMutex
	nodes         map[NodeID]*partitionNodeState
	suspicionTime time.Duration
	partitions    [][]NodeID
}

type partitionNodeState struct {
	ID           NodeID
	LastSeen     time.Time
	CanReach     map[NodeID]bool
	PartitionTag string
}

// NewPartitionDetector creates a partition detector.
func NewPartitionDetector(suspicionTime time.Duration) *PartitionDetector {
	if suspicionTime <= 0 {
		suspicionTime = 10 * time.Second
	}
	return &PartitionDetector{
		nodes:         make(map[NodeID]*partitionNodeState),
		suspicionTime: suspicionTime,
	}
}

// AddNode adds a node to monitor.
func (pd *PartitionDetector) AddNode(id NodeID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()
	pd.nodes[id] = &partitionNodeState{
		ID:       id,
		LastSeen: time.Now(),
		CanReach: make(map[NodeID]bool),
	}
}

// ReportReachable records that fromNode can reach toNode.
func (pd *PartitionDetector) ReportReachable(from, to NodeID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if state, ok := pd.nodes[from]; ok {
		state.LastSeen = time.Now()
		state.CanReach[to] = true
	}
}

// ReportUnreachable records that fromNode cannot reach toNode.
func (pd *PartitionDetector) ReportUnreachable(from, to NodeID) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if state, ok := pd.nodes[from]; ok {
		state.CanReach[to] = false
	}
}

// DetectPartitions analyzes reachability data to find network partitions.
func (pd *PartitionDetector) DetectPartitions() [][]NodeID {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	now := time.Now()
	// Remove nodes that haven't been seen
	active := make(map[NodeID]*partitionNodeState)
	for id, state := range pd.nodes {
		if now.Sub(state.LastSeen) < pd.suspicionTime*3 {
			active[id] = state
		}
	}
	pd.nodes = active

	// Build adjacency graph: two nodes are in the same partition if they can reach each other
	// Use union-find to find connected components
	uf := newUnionFind()
	for id := range active {
		uf.find(string(id))
	}

	for id, state := range active {
		for other, reachable := range state.CanReach {
			if reachable {
				if _, ok := active[other]; ok {
					uf.union(string(id), string(other))
				}
			}
		}
	}

	// Also union nodes that are mutually reachable
	for id, state := range active {
		for other, reachable := range state.CanReach {
			if reachable {
				if otherState, ok := active[other]; ok {
					if otherState.CanReach[id] {
						uf.union(string(id), string(other))
					}
				}
			}
		}
	}

	// Group by root
	groups := make(map[string][]NodeID)
	for id := range active {
		root := uf.find(string(id))
		groups[root] = append(groups[root], id)
	}

	var partitions [][]NodeID
	for _, group := range groups {
		sort.Slice(group, func(i, j int) bool { return group[i] < group[j] })
		partitions = append(partitions, group)
	}

	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i][0] < partitions[j][0]
	})

	pd.partitions = partitions
	return partitions
}

// LastPartitions returns the last detected partitions.
func (pd *PartitionDetector) LastPartitions() [][]NodeID {
	pd.mu.RLock()
	defer pd.mu.RUnlock()
	return pd.partitions
}

// IsPartitioned returns true if there is more than one partition.
func (pd *PartitionDetector) IsPartitioned() bool {
	return len(pd.DetectPartitions()) > 1
}

// ---- Union-Find for partition detection ----

type unionFind struct {
	parent map[string]string
}

func newUnionFind() *unionFind {
	return &unionFind{parent: make(map[string]string)}
}

func (uf *unionFind) find(x string) string {
	if _, ok := uf.parent[x]; !ok {
		uf.parent[x] = x
	}
	if uf.parent[x] != x {
		uf.parent[x] = uf.find(uf.parent[x])
	}
	return uf.parent[x]
}

func (uf *unionFind) union(x, y string) {
	rx := uf.find(x)
	ry := uf.find(y)
	if rx != ry {
		uf.parent[rx] = ry
	}
}

// ---- Cluster Manager (orchestrates everything) ----

// ClusterManager ties together topology, heartbeats, leader election,
// gossip, and partition detection.
type ClusterManager struct {
	Topology   *Topology
	Heartbeats *HeartbeatMonitor
	Election   *BullyElection
	Gossip     *GossipProtocol
	Partitions *PartitionDetector
	self       NodeID
	mu         sync.Mutex
	running    bool
}

// NewClusterManager creates a new cluster manager.
func NewClusterManager(self NodeID, address string, port int) *ClusterManager {
	selfNode := NewNode(address, port)
	selfNode.ID = self

	topo := NewTopology(self)
	topo.AddNode(selfNode)

	hb := NewHeartbeatMonitor(5 * time.Second)
	elect := NewBullyElection(self, nil)
	gossip := NewGossipProtocol(3)
	partition := NewPartitionDetector(10 * time.Second)

	cm := &ClusterManager{
		Topology:   topo,
		Heartbeats: hb,
		Election:   elect,
		Gossip:     gossip,
		Partitions: partition,
		self:       self,
	}

	// Wire up callbacks
	hb.OnTimeout(func(id NodeID) {
		topo.UpdateNodeState(id, StateSuspected)
	})

	hb.OnRecover(func(id NodeID) {
		topo.UpdateNodeState(id, StateAlive)
	})

	return cm
}

// Self returns this node's ID.
func (cm *ClusterManager) Self() NodeID {
	return cm.self
}

// Join adds a peer node to the cluster.
func (cm *ClusterManager) Join(node *Node) {
	cm.Topology.AddNode(node)
	cm.Election.AddNode(node.ID)
	cm.Gossip.AddNode(node.ID)
	cm.Partitions.AddNode(node.ID)
}

// Leave removes a peer from the cluster.
func (cm *ClusterManager) Leave(id NodeID) {
	cm.Topology.UpdateNodeState(id, StateLeft)
	cm.Election.RemoveNode(id)
	cm.Gossip.RemoveNode(id)
}

// SendHeartbeat sends a heartbeat for this node.
func (cm *ClusterManager) SendHeartbeat(load float64) {
	hb := &Heartbeat{
		NodeID:    cm.self,
		Timestamp: time.Now(),
		Sequence:  0,
		Load:      load,
	}
	cm.Heartbeats.ReceiveHeartbeat(hb)
}

// StartElection triggers leader election.
func (cm *ClusterManager) StartElection() NodeID {
	return cm.Election.StartElection()
}

// Leader returns the current leader.
func (cm *ClusterManager) Leader() NodeID {
	return cm.Election.Leader()
}

// GossipMessage sends data via gossip.
func (cm *ClusterManager) GossipMessage(data map[string]string, ttl int) {
	cm.Gossip.Gossip(cm.self, data, ttl)
}

// CheckPartitions runs partition detection.
func (cm *ClusterManager) CheckPartitions() [][]NodeID {
	return cm.Partitions.DetectPartitions()
}

// ---- Cluster Events ----

// EventType indicates the type of cluster event.
type EventType int

const (
	// EventNodeJoin indicates a node joined.
	EventNodeJoin EventType = iota
	// EventNodeLeave indicates a node left.
	EventNodeLeave
	// EventNodeFail indicates a node failed.
	EventNodeFail
	// EventLeaderChange indicates a new leader was elected.
	EventLeaderChange
	// EventPartition indicates a network partition was detected.
	EventPartition
	// EventHeal indicates a partition was healed.
	EventHeal
)

var eventTypeStrings = map[EventType]string{
	EventNodeJoin:     "node_join",
	EventNodeLeave:    "node_leave",
	EventNodeFail:     "node_fail",
	EventLeaderChange: "leader_change",
	EventPartition:    "partition",
	EventHeal:         "heal",
}

func (et EventType) String() string {
	if s, ok := eventTypeStrings[et]; ok {
		return s
	}
	return "unknown"
}

// ClusterEvent represents an event in the cluster.
type ClusterEvent struct {
	Type      EventType
	NodeID    NodeID
	Timestamp time.Time
	Data      map[string]string
}

// EventBus distributes cluster events to listeners.
type EventBus struct {
	mu        sync.RWMutex
	listeners map[int]chan ClusterEvent
	nextID    int
}

// NewEventBus creates an event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		listeners: make(map[int]chan ClusterEvent),
	}
}

// Subscribe returns a channel that receives cluster events.
func (eb *EventBus) Subscribe(bufferSize int) (int, <-chan ClusterEvent) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	id := eb.nextID
	eb.nextID++
	ch := make(chan ClusterEvent, bufferSize)
	eb.listeners[id] = ch
	return id, ch
}

// Unsubscribe removes a listener.
func (eb *EventBus) Unsubscribe(id int) {
	eb.mu.Lock()
	defer eb.mu.Unlock()
	if ch, ok := eb.listeners[id]; ok {
		close(ch)
		delete(eb.listeners, id)
	}
}

// Emit sends an event to all listeners.
func (eb *EventBus) Emit(evt ClusterEvent) {
	eb.mu.RLock()
	defer eb.mu.RUnlock()
	for _, ch := range eb.listeners {
		select {
		case ch <- evt:
		default:
			// Drop if listener is slow
		}
	}
}

// ---- Utilities ----

// FormatTopology returns a human-readable representation of the topology.
func FormatTopology(t *Topology) string {
	nodes := t.ListNodes()
	var result string
	result += fmt.Sprintf("Topology v%d (%d nodes):\n", t.Version(), len(nodes))
	for _, n := range nodes {
		leader := ""
		result += fmt.Sprintf("  %s [%s] @ %s:%d - %s%s\n",
			n.ID, n.Role, n.Address, n.Port, n.State, leader)
	}
	return result
}

// SimulatePartition creates a simulated network partition by manipulating reachability.
func SimulatePartition(pd *PartitionDetector, groupA, groupB []NodeID) {
	// Nodes in groupA cannot reach nodes in groupB
	for _, a := range groupA {
		for _, b := range groupB {
			pd.ReportUnreachable(a, b)
			pd.ReportUnreachable(b, a)
		}
	}
	// Nodes within each group can reach each other
	for _, a := range groupA {
		for _, aa := range groupA {
			if a != aa {
				pd.ReportReachable(a, aa)
			}
		}
	}
	for _, b := range groupB {
		for _, bb := range groupB {
			if b != bb {
				pd.ReportReachable(b, bb)
			}
		}
	}
}

// HealPartition restores connectivity between two groups.
func HealPartition(pd *PartitionDetector, groupA, groupB []NodeID) {
	for _, a := range groupA {
		for _, b := range groupB {
			pd.ReportReachable(a, b)
			pd.ReportReachable(b, a)
		}
	}
}

// ---- Membership List ----

// MembershipList maintains a consistent view of cluster membership.
type MembershipList struct {
	mu      sync.RWMutex
	members map[NodeID]*membershipEntry
}

type membershipEntry struct {
	Node      *Node
	JoinedAt  time.Time
	UpdatedAt time.Time
	Version   int64
}

// NewMembershipList creates a membership list.
func NewMembershipList() *MembershipList {
	return &MembershipList{
		members: make(map[NodeID]*membershipEntry),
	}
}

// Join adds a node to the membership.
func (ml *MembershipList) Join(node *Node) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	now := time.Now()
	if entry, ok := ml.members[node.ID]; ok {
		entry.Node = node
		entry.UpdatedAt = now
		entry.Version++
	} else {
		ml.members[node.ID] = &membershipEntry{
			Node:      node,
			JoinedAt:  now,
			UpdatedAt: now,
			Version:   1,
		}
	}
}

// Leave removes a node.
func (ml *MembershipList) Leave(id NodeID) {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	delete(ml.members, id)
}

// Members returns all member nodes.
func (ml *MembershipList) Members() []*Node {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	members := make([]*Node, 0, len(ml.members))
	for _, entry := range ml.members {
		members = append(members, entry.Node)
	}
	return members
}

// Size returns the number of members.
func (ml *MembershipList) Size() int {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	return len(ml.members)
}

// Contains checks if a node is a member.
func (ml *MembershipList) Contains(id NodeID) bool {
	ml.mu.RLock()
	defer ml.mu.RUnlock()
	_, ok := ml.members[id]
	return ok
}
