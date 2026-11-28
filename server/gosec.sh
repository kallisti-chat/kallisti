#!/bin/sh
# Runs gosec as CI does (.github/workflows/sec.yml): medium severity and
# confidence and up, any finding fails. Extra arguments go to gosec
# (e.g. -fmt json). Needs only Go: gosec is built at the pinned version.
#
# A false positive at one call site: `// #nosec G304 -- why` on that line
# (the rule and the reason are required). A rule that is noise in a whole
# directory or everywhere: below, with its reason.
set -e
cd "$(dirname "$0")"

# Off everywhere.
# G115 (integer conversions): 74 sites when added, each bounded by a check
# or switch gosec does not follow, a protocol's wraparound (RTP, Mumble
# varints) or a uint32 id read back from SQLite.
exclude=G115

# Off by path (regex on the file path, then rules). Development tools only:
# none of them is in the image (the Dockerfile builds ./cmd/server).
# seedchat, warmload: files named by their flags; math/rand for test data;
# warmload talks to a local server's self-signed certificate.
rules='/cmd/(seedchat|warmload)/:G304,G402,G404'
# linkproxy: math/rand for simulated jitter.
rules="$rules;/cmd/linkproxy/:G404"
# fakefedi and fedifake: a fake fediverse instance for tests that approves
# every login, on 127.0.0.1 (no timeouts, redirects and fetches as asked).
rules="$rules;/cmd/fakefedi/:G114;/internal/fedi/fedifake/:G704,G710"
# translation/gen: go generate fetching the model list from fixed URLs and
# writing models.json, which is checked in.
rules="$rules;/internal/translation/gen/:G107,G306"

exec go run github.com/securego/gosec/v2/cmd/gosec@v2.30.0 \
	-severity medium -confidence medium \
	-nosec-require-rules -nosec-require-justification \
	-exclude "$exclude" -exclude-rules "$rules" \
	"$@" ./...
