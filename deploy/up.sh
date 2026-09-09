#!/bin/sh
# Start cmoa serve and the monitor with the repository compose file.
# Usage: CMOA_CONFIG=/path/to/cmoa.json ./deploy/up.sh
# Extra arguments are passed to `docker compose up` (`-d`, `--no-build`, …).
set -eu
cd "$(dirname "$0")/.."

export CMOA_UID="${CMOA_UID:-$(id -u)}"
export CMOA_GID="${CMOA_GID:-$(id -g)}"

if [ -z "${CMOA_CONFIG:-}" ] && [ -f ./cmoa.json ]; then
	CMOA_CONFIG="$(pwd)/cmoa.json"
fi
if [ -n "${CMOA_CONFIG:-}" ]; then
	case "$CMOA_CONFIG" in
	/*) ;;
	*) CMOA_CONFIG="$(pwd)/$CMOA_CONFIG" ;;
	esac
	export CMOA_CONFIG
fi
if [ -z "${CMOA_CONFIG:-}" ]; then
	echo "cmoa: set CMOA_CONFIG to a cmoa.json (or place ./cmoa.json in the repo root)" >&2
	exit 2
fi
if [ ! -f "$CMOA_CONFIG" ]; then
	echo "cmoa: CMOA_CONFIG=$CMOA_CONFIG is not a file" >&2
	exit 2
fi

bind="${CMOA_BIND:-$HOME}"
export CMOA_BIND="$bind"
case "$CMOA_CONFIG" in
"$bind" | "$bind"/*) ;;
*)
	echo "cmoa: $CMOA_CONFIG is outside CMOA_BIND=$bind" >&2
	echo "cmoa: set CMOA_BIND to a common parent of the config, vault and serve.runs_dir" >&2
	exit 2
	;;
esac

# Host network shares loopback with the machine. A leftover `cmoa serve`
# on the same listen address makes the container exit immediately.
if command -v python3 >/dev/null && command -v ss >/dev/null; then
	listen=$(python3 -c '
import json, sys
with open(sys.argv[1], encoding="utf-8") as fh:
    cfg = json.load(fh)
serve = cfg.get("serve") or {}
print(serve.get("listen") or "127.0.0.1:8095")
' "$CMOA_CONFIG")
	port=${listen##*:}
	if [ -n "$port" ] && ss -ltnH "sport = :$port" | grep -q .; then
		echo "cmoa: $listen is already in use (host-network serve cannot bind it twice)" >&2
		echo "cmoa: stop the existing cmoa serve (or whatever owns that port) and retry" >&2
		ss -ltnp "sport = :$port" >&2 || true
		exit 1
	fi
	monitor_port=${CMOA_MONITOR_PORT:-3999}
	if ss -ltnH "sport = :$monitor_port" | grep -q .; then
		echo "cmoa: 0.0.0.0:$monitor_port is already in use (the monitor binds it on the host network)" >&2
		echo "cmoa: stop the process on that port, or set CMOA_MONITOR_PORT" >&2
		ss -ltnp "sport = :$monitor_port" >&2 || true
		exit 1
	fi
fi

exec docker compose up --build "$@"
