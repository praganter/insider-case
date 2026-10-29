#!/bin/sh
# Runs every test with a real Postgres (TEST_DATABASE_URL). A skipped test fails
# the run, so a missing database can never turn the suite silently green.
go test -count=1 -v ./... >/tmp/out 2>&1
rc=$?
if [ $rc -ne 0 ]; then cat /tmp/out; else grep -E "^(ok|\?)" /tmp/out; fi
if grep -q -- "--- SKIP" /tmp/out; then
	echo "skipped tests are not allowed:"; grep -- "--- SKIP" /tmp/out
	exit 1
fi
exit $rc
