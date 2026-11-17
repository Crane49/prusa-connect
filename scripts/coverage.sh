#!/bin/sh
# Runs the unit suite and one in-process integration test with coverage, merges
# the profiles, and writes one text report and one HTML report. `make coverage`
# runs it.
#
# It fails when the integration test does not exist or did not run:
#
#   - `go test -run` exits 0 with `[no tests to run]` for a name no test has,
#     so the script asks `go test -list` for the name first
#   - a skipped test exits 0 too, so the script looks for the test's own
#     `--- PASS` line
#
# A failing suite does not stop the run. The report is written first and the
# failure is returned at the end.
#
# Usage: sh scripts/coverage.sh, with these set in the environment. The
# Makefile sets them all:
#   COVERAGE_UNIT_PROFILE, COVERAGE_INTEGRATION_PROFILE, COVERAGE_REPORT,
#   COVERAGE_HTML, COVERAGE_INTEGRATION_LOG, COVERAGE_INTEGRATION_TAGS,
#   COVERAGE_INTEGRATION_TEST, COVERAGE_INTEGRATION_PKG

for v in COVERAGE_UNIT_PROFILE COVERAGE_INTEGRATION_PROFILE COVERAGE_REPORT \
	COVERAGE_HTML COVERAGE_INTEGRATION_LOG COVERAGE_INTEGRATION_TAGS \
	COVERAGE_INTEGRATION_TEST COVERAGE_INTEGRATION_PKG; do
	eval "value=\$$v"
	if [ -z "$value" ]; then
		echo "FAIL  $v is unset. Run this through 'make coverage', which owns the names."
		exit 2
	fi
done

echo "Running all tests with coverage..."
mkdir -p bin
rm -f "$COVERAGE_REPORT" "$COVERAGE_HTML" "$COVERAGE_UNIT_PROFILE" "$COVERAGE_INTEGRATION_PROFILE"

# Fails when the named test does not exist, for example after a rename.
COLLECTED=$(go test -list "^$COVERAGE_INTEGRATION_TEST\$" $COVERAGE_INTEGRATION_PKG |
	grep -c "^$COVERAGE_INTEGRATION_TEST\$" || true)
if [ "$COLLECTED" -eq 0 ]; then
	echo "FAIL go test collects no $COVERAGE_INTEGRATION_TEST in $COVERAGE_INTEGRATION_PKG."
	exit 1
fi

echo "  - Unit tests..."
go test -short -coverprofile="$COVERAGE_UNIT_PROFILE" ./...
UNIT=$?

echo "  - Integration tests (in-process)..."
go test -tags "$COVERAGE_INTEGRATION_TAGS" -v \
	-coverprofile="$COVERAGE_INTEGRATION_PROFILE" -coverpkg=./... \
	$COVERAGE_INTEGRATION_PKG -run "^$COVERAGE_INTEGRATION_TEST\$" -short=false \
	> "$COVERAGE_INTEGRATION_LOG" 2>&1
INTEGRATION=$?
cat "$COVERAGE_INTEGRATION_LOG"

# A skipped test exits 0. The run's own PASS line is what says it ran.
if ! grep -q "^--- PASS: $COVERAGE_INTEGRATION_TEST" "$COVERAGE_INTEGRATION_LOG"; then
	echo "FAIL $COVERAGE_INTEGRATION_TEST did not pass, so the profile beside it measures nothing."
	INTEGRATION=1
fi

report() {
	go tool cover -html="$COVERAGE_REPORT" -o "$COVERAGE_HTML" 2>/dev/null
	TOTAL=$(go tool cover -func="$COVERAGE_REPORT" 2>/dev/null) || return $?
	printf '%s\n' "$TOTAL" | tail -1
	echo ""
}

if [ -f "$COVERAGE_UNIT_PROFILE" ] && [ -f "$COVERAGE_INTEGRATION_PROFILE" ]; then
	echo "Combining coverage reports..."
	echo "mode: set" > "$COVERAGE_REPORT"
	tail -n +2 "$COVERAGE_UNIT_PROFILE" >> "$COVERAGE_REPORT" 2>/dev/null || true
	tail -n +2 "$COVERAGE_INTEGRATION_PROFILE" >> "$COVERAGE_REPORT" 2>/dev/null || true
	report || exit $?
	rm -f "$COVERAGE_UNIT_PROFILE" "$COVERAGE_INTEGRATION_PROFILE"
	echo "OK Combined coverage report: $COVERAGE_HTML"
elif [ -f "$COVERAGE_UNIT_PROFILE" ]; then
	mv "$COVERAGE_UNIT_PROFILE" "$COVERAGE_REPORT"
	report || exit $?
	echo "OK Coverage report: $COVERAGE_HTML"
elif [ -f "$COVERAGE_INTEGRATION_PROFILE" ]; then
	mv "$COVERAGE_INTEGRATION_PROFILE" "$COVERAGE_REPORT"
	report || exit $?
	echo "OK Coverage report: $COVERAGE_HTML"
else
	echo "WARN No coverage data generated"
fi

if [ "$UNIT" -ne 0 ] || [ "$INTEGRATION" -ne 0 ]; then
	echo ""
	echo "FAIL The coverage report was written, but the run under it failed (unit=$UNIT integration=$INTEGRATION)."
	exit 1
fi
