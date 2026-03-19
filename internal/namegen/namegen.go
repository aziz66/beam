package namegen

import (
	"crypto/rand"
	"math/big"
	"regexp"
)

var adjectives = []string{
	"amber", "aqua", "arctic", "autumn", "azure",
	"berry", "blaze", "bloom", "bold", "brave",
	"breeze", "bright", "bronze", "calm", "cedar",
	"cherry", "chill", "citrus", "clear", "cliff",
	"cloud", "clover", "cobalt", "cool", "copper",
	"coral", "cosmic", "cozy", "creek", "crisp",
	"crystal", "cyan", "dapper", "dawn", "deep",
	"delta", "desert", "dewy", "drift", "dusk",
	"dusty", "eager", "earth", "echo", "ember",
	"emerald", "even", "fair", "fern", "fierce",
	"fire", "flash", "fleet", "flint", "flora",
	"foggy", "forest", "fossil", "fresh", "frost",
	"gentle", "ginger", "glacier", "gleam", "glen",
	"glow", "golden", "grand", "granite", "grape",
	"green", "grove", "hazel", "honey", "humble",
	"icy", "idle", "indigo", "iron", "island",
	"ivory", "jade", "jasper", "jolly", "keen",
	"lapis", "lark", "lava", "leaf", "lemon",
	"light", "lilac", "lime", "linen", "lofty",
	"lotus", "lucky", "lunar", "lush", "lyric",
	"maple", "marsh", "mellow", "mesa", "mist",
	"mocha", "moon", "mossy", "navy", "neat",
	"noble", "north", "nova", "oak", "oasis",
	"ocean", "olive", "onyx", "opal", "orbit",
	"orchid", "pale", "palm", "pearl", "pebble",
	"peony", "pine", "pixel", "plain", "plum",
	"polar", "pond", "poppy", "prairie", "prism",
	"pure", "quartz", "quick", "quiet", "rain",
	"rapid", "raven", "reef", "ridge", "river",
	"robin", "rocky", "rose", "royal", "ruby",
	"rustic", "sage", "sand", "satin", "scarlet",
	"serene", "shadow", "sharp", "shell", "shore",
	"silent", "silk", "silver", "sky", "slate",
	"sleek", "snow", "solar", "solid", "spark",
	"spice", "spring", "steel", "stone", "storm",
	"stream", "sugar", "summit", "sunny", "surf",
	"swift", "teal", "tender", "terra", "thistle",
	"thorn", "thunder", "tidal", "timber", "topaz",
	"trail", "tulip", "turbo", "velvet", "verde",
	"violet", "vivid", "warm", "wave", "west",
	"wild", "willow", "wind", "winter", "wise",
	"wood", "zephyr", "zinc", "misty", "dusty",
}

var nouns = []string{
	"albatross", "anchor", "antler", "arch", "badger",
	"bamboo", "basin", "beacon", "bear", "birch",
	"bison", "bloom", "bluff", "boat", "bobcat",
	"boulder", "bridge", "brook", "bunny", "butte",
	"butterfly", "cabin", "canary", "canyon", "cardinal",
	"castle", "cave", "cedar", "cloud", "coast",
	"cobble", "condor", "coral", "cottage", "cougar",
	"cove", "crane", "creek", "crest", "crow",
	"crystal", "cypress", "dale", "dawn", "deer",
	"delta", "dolphin", "dove", "drift", "drum",
	"dune", "eagle", "echo", "elk", "elm",
	"ember", "falcon", "fawn", "ferry", "field",
	"finch", "fjord", "flame", "flint", "flora",
	"forge", "fossil", "fountain", "fox", "frost",
	"garden", "gate", "gazelle", "glacier", "glen",
	"gorge", "grove", "harbor", "hare", "haven",
	"hawk", "hearth", "hedge", "heron", "hill",
	"hollow", "horizon", "hound", "island", "ivy",
	"jackdaw", "jasmine", "jay", "jewel", "juniper",
	"kayak", "kestrel", "kettle", "kinglet", "knoll",
	"lagoon", "lake", "lantern", "lark", "laurel",
	"ledge", "leopard", "lily", "linden", "lodge",
	"lotus", "lynx", "maple", "marble", "marsh",
	"meadow", "mesa", "minnow", "mint", "mirror",
	"moon", "moss", "moth", "mountain", "muse",
	"narwhal", "needle", "nest", "newt", "north",
	"nutmeg", "oak", "oasis", "orchid", "osprey",
	"otter", "owl", "panther", "parrot", "path",
	"peak", "pearl", "pelican", "petal", "phoenix",
	"pine", "plover", "plume", "pond", "prairie",
	"puma", "quail", "quarry", "rabbit", "raccoon",
	"rain", "rapids", "raven", "reef", "ridge",
	"ripple", "river", "robin", "rock", "rose",
	"sage", "salmon", "sand", "seal", "sequoia",
	"shade", "shell", "shore", "sierra", "skylark",
	"slope", "snipe", "snow", "sparrow", "spring",
	"spruce", "star", "stone", "stork", "stream",
	"summit", "swan", "temple", "thistle", "thorn",
	"thrush", "thunder", "tide", "tiger", "timber",
	"tower", "trail", "trout", "tulip", "turtle",
	"valley", "violet", "viper", "vista", "walrus",
	"wander", "wave", "weasel", "whale", "willow",
	"wind", "wolf", "wren", "zenith", "zephyr",
}

var codePattern = regexp.MustCompile(`^[a-z]+-[a-z]+-\d{2}$`)

func Generate() string {
	adj := adjectives[randInt(len(adjectives))]
	noun := nouns[randInt(len(nouns))]
	num := randInt(90) + 10 // 10-99
	return adj + "-" + noun + "-" + itoa(num)
}

func Validate(code string) bool {
	return codePattern.MatchString(code)
}

func randInt(max int) int {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return int(n.Int64())
}

func itoa(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
