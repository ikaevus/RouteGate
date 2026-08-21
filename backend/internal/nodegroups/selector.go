package nodegroups

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
)

const (
	SelectionReasonPriority = "lowest_priority"
	SelectionReasonWeighted = "weighted_rendezvous"
	SelectionReasonReady    = "ready_candidates_preferred"
	SelectionReasonDegraded = "degraded_fallback"
)

type CandidateSelection struct {
	Candidate NodeGroupCandidate
	Reasons   []string
}

// SelectCandidate chooses one stable, explainable candidate. Ready nodes always
// form the preferred pool; degraded nodes are only considered as a fallback.
func SelectCandidate(accountID, strategy string, candidates []NodeGroupCandidate, allowDegraded bool) (CandidateSelection, bool) {
	ready := make([]NodeGroupCandidate, 0, len(candidates))
	degraded := make([]NodeGroupCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Eligible {
			continue
		}
		switch candidate.Health {
		case CandidateHealthReady:
			ready = append(ready, candidate)
		case CandidateHealthDegraded:
			degraded = append(degraded, candidate)
		}
	}

	pool := ready
	reasons := []string{SelectionReasonReady}
	if len(pool) == 0 && allowDegraded {
		pool = degraded
		reasons = []string{SelectionReasonDegraded}
	}
	if len(pool) == 0 {
		return CandidateSelection{}, false
	}

	if strategy == SelectionStrategyWeighted {
		selected := pool[0]
		best := weightedRendezvousScore(accountID, selected.ServerID, selected.Weight)
		for _, candidate := range pool[1:] {
			score := weightedRendezvousScore(accountID, candidate.ServerID, candidate.Weight)
			if score < best || (score == best && candidate.ServerID < selected.ServerID) {
				selected, best = candidate, score
			}
		}
		return CandidateSelection{Candidate: selected, Reasons: append(reasons, SelectionReasonWeighted)}, true
	}

	sort.Slice(pool, func(i, j int) bool {
		if pool[i].Priority != pool[j].Priority {
			return pool[i].Priority < pool[j].Priority
		}
		return pool[i].ServerID < pool[j].ServerID
	})
	return CandidateSelection{Candidate: pool[0], Reasons: append(reasons, SelectionReasonPriority)}, true
}

func weightedRendezvousScore(accountID, serverID string, weight int) float64 {
	if weight < 1 {
		weight = 1
	}
	sum := sha256.Sum256([]byte(accountID + "\x00" + serverID))
	raw := binary.BigEndian.Uint64(sum[:8])
	unit := (float64(raw) + 1) / (float64(math.MaxUint64) + 1)
	return -math.Log(unit) / float64(weight)
}
