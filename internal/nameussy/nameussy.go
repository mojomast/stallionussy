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
	// Original 32
	"Midnight", "Dark", "Suspicious", "Turbulent", "Flannel",
	"Sapphic", "Anomalous", "Thunderous", "Volatile", "Reckless",
	"Organic", "Mediterranean", "Cryogenic", "Distinguished", "Autonomous",
	"Concurrent", "Distributed", "Velvet", "Forbidden", "Haunted",
	"Legendary", "Certified", "Premium", "Artisanal", "Blessed",
	"Chaotic", "Elegant", "Furious", "Gentle", "Hyperbolic",
	"Iridescent", "Quantum",
	// Tech & DevOps
	"Containerized", "Serverless", "Idempotent", "Asynchronous", "Ephemeral",
	"Stateless", "Orchestrated", "Polymorphic", "Declarative", "Observable",
	"Immutable", "Decoupled", "Horizontally-Scaled", "Event-Driven", "Cloud-Native",
	// Ussy lore & Sappho Scale
	"Quarantined", "Contained", "Prophetic", "Poetic", "Oracular",
	"Yogurt-Based", "Flannel-Wrapped", "Anomalous-Grade", "Sappho-Certified", "Lesbian",
	// Comedy & vibes
	"Unhinged", "Feral", "Cottagecore", "Gaslit", "Girlboss",
	"Gatekept", "Chronically-Online", "Touch-Grass", "Main-Character", "Situationship",
	"Delulu", "Rizzy", "Slay-Adjacent", "Unhinged", "Goblin-Mode",
	// Corporate buzzwords
	"Synergistic", "Disruptive", "Scalable", "Leveraged", "Agile",
	"Enterprise-Grade", "Mission-Critical", "Blockchain-Enabled", "AI-Powered", "Web3-Native",
	// Absurd descriptors
	"Sentient", "Unlicensed", "Tax-Exempt", "Legally-Distinct", "Peer-Reviewed",
	"FDA-Unapproved", "Paranormal", "Load-Bearing", "Cryptographic", "Biodynamic",
}

var nouns = []string{
	// Original 30
	"Deploy", "Thunder", "Legacy", "Yogurt", "Hummus",
	"Stallion", "Sunset", "Flannel", "Pipeline", "Daemon",
	"Goroutine", "Rollback", "Hotfix", "Merger", "Dividend",
	"Vial", "Sunrise", "Moonshine", "Biscuit", "Conquest",
	"Verdict", "Fortune", "Horizon", "Whisper", "Tempest",
	"Oracle", "Phantom", "Reckoning", "Catalyst", "Entropy",
	// Tech & DevOps
	"Kubernetes", "Docker", "Terraform", "Ansible", "Prometheus",
	"Grafana", "Nginx", "Redis", "Postgres", "Webhook",
	"Middleware", "Microservice", "Serverless", "Lambda", "Incident",
	"Downtime", "Latency", "Throughput", "Payload", "Endpoint",
	// Ussy lore
	"Anomaly", "Containment", "Quarantine", "Prophecy", "Poetry",
	"Sappho", "Lesbos", "Fragment", "Stanza", "Ode",
	"Vortex", "Breach", "Specimen", "Artifact", "Protocol",
	// B.U.R.P. & cryptids
	"Mothman", "Jackalope", "Bigfoot", "Chupacabra", "Skinwalker",
	"Wendigo", "Thunderbird", "Jersey-Devil", "Flatwoods-Monster", "Mokele-Mbembe",
	// Comedy & millennial
	"Avocado-Toast", "Sourdough", "Kombucha", "Side-Hustle", "Situationship",
	"Burnout", "Brunch", "Crypto", "NFT", "Discourse",
	"Hot-Take", "Vibes", "Manifesto", "Audacity", "Spreadsheet",
}

var names = []string{
	// Original 19
	"Jason", "Derulo", "Mittens", "Geoffrussy", "Nessie",
	"Sappho", "Mothman", "Jackalope", "Flannelsworth", "Router",
	"Ethernet", "Thundercock", "Hummus", "Tahini", "Pastor",
	"Margaret", "Chen", "Bitcoin", "Subaru",
	// Ussyverse lore characters
	"Dr. Mittens", "Jason Derulo", "Pastor Router", "E-008", "Geoffrussy",
	// B.U.R.P. agents & cryptids
	"Bigfoot", "Chupacabra", "Flatwoods", "Skinwalker", "Wendigo",
	"Agent Flannel", "Agent Yogurt", "Director Sappho", "Intern Kevin",
	// Tech names
	"Kubernetes", "Docker", "Terraform", "Jenkins", "Prometheus",
	"Grafana", "Nginx", "Redis", "Postgres", "Linus",
	// Comedy names
	"Sourdough", "Kombucha", "Avocado", "Blockchain", "Crypto",
	"Cottagecore", "Girlboss", "Gatekeep", "Gaslight", "Brunch",
	"Spreadsheet", "LinkedIn", "Slack", "Jira",
}

var suffixes = []string{
	// Original 16
	"III", "IV", "IX", "Jr", "Sr",
	"PhD", "DVM", "Esq", "MBA",
	"the Bold", "the Haunted", "the Smooth",
	"the Volatile", "the Distributed",
	"of Lesbos", "of Delaware",
	// Royal & noble
	"the Magnificent", "the Terrible", "the Unhinged", "the Feral",
	"the Blessed", "the Cursed", "the Anomalous", "the Quarantined",
	"the Sapphic", "the Eternal",
	// Academic & corporate
	"MD", "CTO", "CEO", "CFO", "LCSW",
	"the Consultant", "the Intern", "the Founder",
	// Lore-specific
	"of the Sappho Scale", "of B.U.R.P.", "of the Ussyverse",
	"the Contained", "the Prophesied", "the Yogurt-Touched",
	"the Serverless", "the Containerized", "the Orchestrated",
	// Comedy
	"(Disputed)", "(Allegedly)", "(Unlicensed)", "(Rotating)",
	"(In Beta)", "(Deprecated)", "(Fork)",
}

var gitCmds = []string{
	// Original 15
	"push", "pull", "rebase", "cherry-pick", "blame",
	"stash", "bisect", "merge", "commit", "reset",
	"reflog", "revert", "fetch", "clone", "checkout",
	// Expanded git commands
	"squash", "amend", "log", "diff", "tag",
	"branch", "init", "remote", "submodule", "worktree",
	"gc", "fsck",
}

var flags = []string{
	// Original 12
	"force", "no-verify", "hard", "soft", "recursive",
	"verbose", "yolo", "production", "friday", "no-rollback",
	"untested", "vibes-only",
	// Expanded flags
	"dry-run", "delete", "prune", "orphan", "squash",
	"no-edit", "allow-empty", "no-ff", "abort", "continue",
	"skip", "all", "cached", "staged", "no-backup",
}

var foods = []string{
	// Original 14
	"Hummus", "Tahini", "Yogurt", "Biscuit", "Oatmilk",
	"Sourdough", "Kombucha", "Avocado", "Kimchi", "Quinoa",
	"Sriracha", "Miso", "Tempeh", "Falafel",
	// Millennial & hipster foods
	"Matcha", "Acai", "Boba", "Oat-Latte", "Cold-Brew",
	"Chia-Pudding", "Toast", "Gochujang", "Halloumi", "Shakshuka",
	"Labneh", "Za'atar", "Harissa", "Nutritional-Yeast", "Seitan",
	// Comfort & absurd
	"Casserole", "Hotdish", "Cobbler", "Grits", "Cornbread",
	"Jambalaya", "Gumbo", "Brisket", "Kolache", "Pierogi",
	"Croissant", "Brioche",
}

var actions = []string{
	// Original 10
	"Uprising", "Deployment", "Convergence", "Reckoning",
	"Awakening", "Ascension", "Migration", "Revolution",
	"Communion", "Optimization",
	// Tech operations
	"Refactoring", "Rollback", "Incident", "Outage", "Postmortem",
	// Lore & drama
	"Containment", "Quarantine", "Prophecy", "Summoning", "Transmutation",
	"Manifestation", "Rapture", "Schism", "Tribunal", "Reckoning",
	"Purge",
}

// Verbs are derived from nouns/actions that sound funny when gerund-ified.
var verbs = []string{
	// Original 30
	"Deploying", "Rebasing", "Merging", "Syncing", "Composting",
	"Fermenting", "Optimizing", "Migrating", "Summoning", "Refactoring",
	"Manifesting", "Compiling", "Unionizing", "Yeeting", "Serializing",
	"Disrupting", "Liquidating", "Ascending", "Converging", "Decrypting",
	"Hydrating", "Benchmarking", "Gaslighting", "Gate-keeping", "Girlbossing",
	"Pivoting", "Sharding", "Throttling", "Negotiating", "Containerizing",
	// Tech & DevOps verbs
	"Orchestrating", "Provisioning", "Terraforming", "Dockerizing", "Load-Balancing",
	"Auto-Scaling", "Monitoring", "Alerting", "Paging", "Debugging",
	"Linting", "Fuzzing", "Profiling", "Hot-Reloading", "Blue-Greening",
	// Ussy lore verbs
	"Containing", "Quarantining", "Prophesying", "Poeticizing", "Sappho-Scaling",
	"Anomaly-Detecting", "Yogurt-Culturing", "Flannel-Wrapping", "Lore-Keeping",
	// Comedy & millennial verbs
	"Doom-Scrolling", "Touch-Grassing", "Quiet-Quitting", "Rage-Applying", "Micro-Dosing",
	"Trauma-Bonding", "Love-Bombing", "Soft-Launching", "Hard-Launching", "Deinfluencing",
	"Romanticizing", "Unsubscribing", "Doomposting", "Ratio-ing",
}

// --------------------------------------------------------------------------
// Stable-specific word lists
// --------------------------------------------------------------------------

var stableTypes = []string{
	// Original 10
	"Stables", "Ranch", "Farms", "Acres", "Meadows",
	"Pastures", "Holdings", "Corral", "Estate", "Paddock",
	// Absurd facility types
	"Containment Zone", "Research Lab", "Observatory", "Compound",
	"Bunker", "Sanctuary", "Collective", "Commune", "Co-op",
	"Accelerator", "Incubator", "Think Tank", "War Room",
	"Dimension", "Annex", "Black Site", "Field Office",
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

// patternUssy: "Thunderussy" — The Suffix Law demands all things end in -ussy.
func patternUssy() string {
	// Pick from adjectives or nouns for the base word, then slap -ussy on it.
	baseWords := []string{
		"Thunder", "Deploy", "Kubernetes", "Flannel", "Yogurt",
		"Docker", "Terraform", "Midnight", "Quantum", "Sapphic",
		"Legacy", "Pipeline", "Daemon", "Hotfix", "Merger",
		"Sourdough", "Kombucha", "Avocado", "Mothman", "Nessie",
		"Prophecy", "Grafana", "Redis", "Nginx", "Postgres",
		"Containment", "Anomaly", "Sappho", "Cottagecore", "Crypto",
		"Girlboss", "Gaslight", "Gatekeep", "Brunch", "Audit",
	}
	return fmt.Sprintf("%sussy", pick(baseWords))
}

// patternBURP: "B.U.R.P. Case #4827: Yogurt" — official investigation codenames
// from the Bureau of Unexplained Racing Phenomena.
func patternBURP() string {
	caseNum := rand.IntN(9999) + 1
	return fmt.Sprintf("B.U.R.P. Case #%d: %s", caseNum, pick(nouns))
}

// patternPastor: "Pastor Mothman's Sourdough" — pastoral/food combos blessed
// by the most unhinged clergy in the Ussyverse.
func patternPastor() string {
	return fmt.Sprintf("Pastor %s's %s", pick(names), pick(foods))
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
	patternUssy,
	patternBURP,
	patternPastor,
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
