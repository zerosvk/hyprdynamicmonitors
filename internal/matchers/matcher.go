// Package matchers provides profile matching logic based on monitor conditions.
package matchers

import (
	"slices"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/power"
	"github.com/sirupsen/logrus"
)

type Matcher struct{}

func NewMatcher() *Matcher {
	return &Matcher{}
}

func (m *Matcher) Match(cfg *config.RawConfig, connectedMonitors []*hypr.MonitorSpec,
	powerState power.PowerState, lidState power.LidState,
) (bool, *MatchedProfile, error) {
	score := make(map[string]int)
	profileRules := make(map[string]map[int]*config.RequiredMonitor)
	profiles := cfg.Profiles
	ok, fallbackProfile := m.returnNoneOrFallback(cfg)
	for name := range profiles {
		score[name] = 0
	}

	for name, profile := range profiles {
		conditions := profile.Conditions
		fullMatchScore := m.calcFullProfileScore(cfg, conditions)
		profileScore, profileRule := m.scoreProfile(cfg, conditions, powerState, lidState, connectedMonitors)
		score[name] = profileScore
		profileRules[name] = profileRule
		logrus.Debugf("Profile %s score %d, full match %d", name, score[name], fullMatchScore)

		// if there is a partial match discard the config
		if fullMatchScore != score[name] {
			delete(score, name)
		}
	}

	bestScore := 0
	for _, value := range score {
		bestScore = max(bestScore, value)
	}

	// when nothing scored > 0 then no config matches
	if bestScore == 0 {
		return ok, NewFallbackProfile(fallbackProfile), nil
	}

	// match from the last entry in the toml config
	ascProfiles := cfg.OrderedProfileKeys()
	slices.Reverse(ascProfiles)
	for _, name := range ascProfiles {
		profile := cfg.Profiles[name]
		if score[name] == bestScore {
			return true, NewMatchedProfile(profile, profileRules[name]), nil
		}
	}
	return ok, NewFallbackProfile(fallbackProfile), nil
}

func (m *Matcher) returnNoneOrFallback(cfg *config.RawConfig) (bool, *config.Profile) {
	if cfg.FallbackProfile != nil {
		return true, cfg.FallbackProfile
	}
	return false, nil
}

func (m *Matcher) scoreProfile(cfg *config.RawConfig, conditions *config.ProfileCondition,
	powerState power.PowerState, lidState power.LidState, connectedMonitors []*hypr.MonitorSpec,
) (int, map[int]*config.RequiredMonitor) {
	monitorToRule := make(map[int]*config.RequiredMonitor)
	profileScore := 0
	if conditions.PowerState != nil && conditions.PowerState.Value() == powerState.String() {
		profileScore += *cfg.Scoring.PowerStateMatch
	}

	if conditions.LidState != nil && conditions.LidState.Value() == lidState.String() {
		profileScore += *cfg.Scoring.LidStateMatch
	}

	// a monitor matches a rule only when every attribute the rule defines matches,
	// so a matched rule always earns its full score; build the candidate monitors per rule
	candidates := make([][]int, len(conditions.RequiredMonitors))
	ruleScores := make([]int, len(conditions.RequiredMonitors))
	for ruleIdx, condition := range conditions.RequiredMonitors {
		for monitorIdx, connectedMonitor := range connectedMonitors {
			score := m.ruleScore(cfg, condition, connectedMonitor)
			if score == 0 {
				continue
			}
			candidates[ruleIdx] = append(candidates[ruleIdx], monitorIdx)
			ruleScores[ruleIdx] = score
		}
	}

	// assign each rule a distinct monitor; greedy first (keeps the first matching monitor
	// in hyprland order), then augmenting paths so that an earlier rule that took a monitor
	// another rule needs can move to a different one, e.g. rules "DP-.*" and "DP-1"
	assignment := assignRules(candidates, len(connectedMonitors))
	for ruleIdx, monitorIdx := range assignment {
		if monitorIdx < 0 {
			continue
		}
		condition := conditions.RequiredMonitors[ruleIdx]
		monitorID := *connectedMonitors[monitorIdx].ID
		logrus.WithFields(logrus.Fields{
			"monitor_id":   monitorID,
			"monitor_name": connectedMonitors[monitorIdx].Name,
			"rule_index":   ruleIdx,
		}).Debug("Monitor assigned to rule")
		profileScore += ruleScores[ruleIdx]
		monitorToRule[monitorID] = condition
	}

	return profileScore, monitorToRule
}

// ruleScore returns the score of a monitor for a rule, 0 when it does not match;
// when the rule defines both name and description both have to match
func (m *Matcher) ruleScore(cfg *config.RawConfig, condition *config.RequiredMonitor,
	connectedMonitor *hypr.MonitorSpec,
) int {
	if condition.HasName() && condition.HasDescription() &&
		(!condition.MatchName(connectedMonitor.Name) ||
			!condition.MatchDescription(connectedMonitor.Description)) {
		return 0
	}

	score := 0
	if condition.HasName() && condition.MatchName(connectedMonitor.Name) {
		score += *cfg.Scoring.NameMatch
	}
	if condition.HasDescription() && condition.MatchDescription(connectedMonitor.Description) {
		score += *cfg.Scoring.DescriptionMatch
	}
	return score
}

// assignRules finds a maximum matching between rules and monitors (each monitor used at most once).
// candidates[rule] lists the matching monitor indexes in preference order. Returns, per rule, the
// assigned monitor index or -1.
func assignRules(candidates [][]int, monitorCount int) []int {
	ruleOf := make([]int, monitorCount) // monitor -> rule
	for i := range ruleOf {
		ruleOf[i] = -1
	}
	monitorOf := make([]int, len(candidates)) // rule -> monitor
	for i := range monitorOf {
		monitorOf[i] = -1
	}

	var try func(rule int, visited []bool) bool
	try = func(rule int, visited []bool) bool {
		// prefer a free monitor so that assignments only move when they have to
		for _, monitor := range candidates[rule] {
			if ruleOf[monitor] == -1 {
				ruleOf[monitor] = rule
				monitorOf[rule] = monitor
				return true
			}
		}
		for _, monitor := range candidates[rule] {
			if visited[monitor] {
				continue
			}
			visited[monitor] = true
			if try(ruleOf[monitor], visited) {
				ruleOf[monitor] = rule
				monitorOf[rule] = monitor
				return true
			}
		}
		return false
	}

	for rule := range candidates {
		try(rule, make([]bool, monitorCount))
	}
	return monitorOf
}

func (m *Matcher) calcFullProfileScore(cfg *config.RawConfig, conditions *config.ProfileCondition) int {
	fullMatchScore := 0
	if conditions.PowerState != nil {
		fullMatchScore += *cfg.Scoring.PowerStateMatch
	}

	if conditions.LidState != nil {
		fullMatchScore += *cfg.Scoring.LidStateMatch
	}

	for _, condition := range conditions.RequiredMonitors {
		if condition.HasName() {
			fullMatchScore += *cfg.Scoring.NameMatch
		}
		if condition.HasDescription() {
			fullMatchScore += *cfg.Scoring.DescriptionMatch
		}
	}

	return fullMatchScore
}
