// Package nameussy generates ridiculous horse and stable names for the
// StallionUSSY horse-genetics trading simulator. Every horse deserves a name
// that makes the announcer question their career choices.
package nameussy

import (
	"fmt"
	"math/rand/v2"
)

// --------------------------------------------------------------------------
// Word lists — curated for maximum comedic density.
// --------------------------------------------------------------------------

var adjectives = []string{
	"Midnight", "Dark", "Suspicious", "Turbulent", "Flannel",
	"Sapphic", "Anomalous", "Thunderous", "Volatile", "Reckless",
	"Organic", "Mediterranean", "Cryogenic", "Distinguished", "Autonomous",
	"Concurrent", "Distributed", "Velvet", "Forbidden", "Haunted",
	"Legendary", "Certified", "Premium", "Artisanal", "Blessed",
	"Chaotic", "Elegant", "Furious", "Gentle", "Hyperbolic",
	"Iridescent", "Quantum",
}

var nouns = []string{
	"Deploy", "Thunder", "Legacy", "Yogurt", "Hummus",
	"Stallion", "Sunset", "Flannel", "Pipeline", "Daemon",
	"Goroutine", "Rollback", "Hotfix", "Merger", "Dividend",
	"Vial", "Sunrise", "Moonshine", "Biscuit", "Conquest",
	"Verdict", "Fortune", "Horizon", "Whisper", "Tempest",
	"Oracle", "Phantom", "Reckoning", "Catalyst", "Entropy",
}

var names = []string{
	"Jason", "Derulo", "Mittens", "Geoffrussy", "Nessie",
	"Sappho", "Mothman", "Jackalope", "Flannelsworth", "Router",
	"Ethernet", "Thundercock", "Hummus", "Tahini", "Pastor",
	"Margaret", "Chen", "Bitcoin", "Subaru",
}

var suffixes = []string{
	"III", "IV", "IX", "Jr", "Sr",
	"PhD", "DVM", "Esq", "MBA",
	"the Bold", "the Haunted", "the Smooth",
	"the Volatile", "the Distributed",
	"of Lesbos", "of Delaware",
}

var gitCmds = []string{
	"push", "pull", "rebase", "cherry-pick", "blame",
	"stash", "bisect", "merge", "commit", "reset",
	"reflog", "revert", "fetch", "clone", "checkout",
}

var flags = []string{
	"force", "no-verify", "hard", "soft", "recursive",
	"verbose", "yolo", "production", "friday", "no-rollback",
	"untested", "vibes-only",
}

var foods = []string{
	"Hummus", "Tahini", "Yogurt", "Biscuit", "Oatmilk",
	"Sourdough", "Kombucha", "Avocado", "Kimchi", "Quinoa",
	"Sriracha", "Miso", "Tempeh", "Falafel",
}

var actions = []string{
	"Uprising", "Deployment", "Convergence", "Reckoning",
	"Awakening", "Ascension", "Migration", "Revolution",
	"Communion", "Optimization",
}

// Verbs are derived from nouns/actions that sound funny when gerund-ified.
var verbs = []string{
	"Deploying", "Rebasing", "Merging", "Syncing", "Composting",
	"Fermenting", "Optimizing", "Migrating", "Summoning", "Refactoring",
	"Manifesting", "Compiling", "Unionizing", "Yeeting", "Serializing",
	"Disrupting", "Liquidating", "Ascending", "Converging", "Decrypting",
	"Hydrating", "Benchmarking", "Gaslighting", "Gate-keeping", "Girlbossing",
	"Pivoting", "Sharding", "Throttling", "Negotiating", "Containerizing",
}

// --------------------------------------------------------------------------
// Stable-specific word lists
// --------------------------------------------------------------------------

var stableTypes = []string{
	"Stables", "Ranch", "Farms", "Acres", "Meadows",
	"Pastures", "Holdings", "Corral", "Estate", "Paddock",
}

// --------------------------------------------------------------------------
// Helpers
// --------------------------------------------------------------------------

// pick returns a random element from the given slice.
func pick(list []string) string {
	return list[rand.IntN(len(list))]
}

// --------------------------------------------------------------------------
// Horse name patterns — each returns a ridiculous name.
// --------------------------------------------------------------------------

// patternAdjectiveNoun: "Midnight Thunder"
func patternAdjectiveNoun() string {
	return fmt.Sprintf("%s %s", pick(adjectives), pick(nouns))
}

// patternPossessive: "Jason's Regret"
func patternPossessive() string {
	return fmt.Sprintf("%s's %s", pick(names), pick(nouns))
}

// patternGerund: "Deploying Hope"
func patternGerund() string {
	return fmt.Sprintf("%s %s", pick(verbs), pick(nouns))
}

// patternSir: "Sir Fluffington IX"
func patternSir() string {
	return fmt.Sprintf("Sir %s %s", pick(names), pick(suffixes))
}

// patternDoubleAdjective: "Dark Suspicious Yogurt"
func patternDoubleAdjective() string {
	a1 := pick(adjectives)
	a2 := pick(adjectives)
	// Re-roll once if we got the same adjective twice — we have standards.
	if a2 == a1 {
		a2 = pick(adjectives)
	}
	return fmt.Sprintf("%s %s %s", a1, a2, pick(nouns))
}

// patternGit: "git push --force"
func patternGit() string {
	return fmt.Sprintf("git %s --%s", pick(gitCmds), pick(flags))
}

// patternFood: "Hummus Uprising"
func patternFood() string {
	return fmt.Sprintf("%s %s", pick(foods), pick(actions))
}

// allPatterns holds every horse-name generator function with equal weight.
var allPatterns = []func() string{
	patternAdjectiveNoun,
	patternPossessive,
	patternGerund,
	patternSir,
	patternDoubleAdjective,
	patternGit,
	patternFood,
}

// GenerateName returns a randomly assembled, announcer-unfriendly horse name.
func GenerateName() string {
	return allPatterns[rand.IntN(len(allPatterns))]()
}

// --------------------------------------------------------------------------
// Stable name patterns
// --------------------------------------------------------------------------

// stableAdjectiveNoun: "Forbidden Pipeline Stables"
func stableAdjectiveNoun() string {
	return fmt.Sprintf("%s %s %s", pick(adjectives), pick(nouns), pick(stableTypes))
}

// stablePossessiveAdjective: "Mothman's Cryogenic Ranch"
func stablePossessiveAdjective() string {
	return fmt.Sprintf("%s's %s %s", pick(names), pick(adjectives), pick(stableTypes))
}

// stableFoodThemed: "Kombucha Meadows"
func stableFoodThemed() string {
	return fmt.Sprintf("%s %s", pick(foods), pick(stableTypes))
}

// stableDoubleNoun: "Daemon & Entropy Farms"
func stableDoubleNoun() string {
	n1 := pick(nouns)
	n2 := pick(nouns)
	if n2 == n1 {
		n2 = pick(nouns)
	}
	return fmt.Sprintf("%s & %s %s", n1, n2, pick(stableTypes))
}

// stableQuantum: "Quantum Yogurt Holdings LLC"
func stableQuantum() string {
	return fmt.Sprintf("%s %s %s LLC", pick(adjectives), pick(nouns), pick(stableTypes))
}

// allStablePatterns holds every stable-name generator function.
var allStablePatterns = []func() string{
	stableAdjectiveNoun,
	stablePossessiveAdjective,
	stableFoodThemed,
	stableDoubleNoun,
	stableQuantum,
}

// GenerateStableName returns a randomly assembled stable/ranch name that
// sounds like it was founded by a venture capitalist who watched Seabiscuit
// on mushrooms.
func GenerateStableName() string {
	return allStablePatterns[rand.IntN(len(allStablePatterns))]()
}
