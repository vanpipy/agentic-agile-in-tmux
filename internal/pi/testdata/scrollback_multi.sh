#!/usr/bin/env bash
# Mock binary for testing multi-line scrollback capture.
# Writes 10 lines one at a time (20ms apart) then exits. The pacing
# is necessary so each line arrives at x/vt as a separate chunk,
# giving handleOutput a chance to capture scrolled-off lines into
# the scrollback buffer. Without pacing (e.g. one printf + exit),
# all 10 lines arrive in a single Read which races with EOF and
# often loses 7+ lines to scrollback.
#
# (Earlier revisions of this fixture used 'sleep 60' after a single
# printf to keep the PTY alive, or exited immediately — both
# produced flaky scrollback captures. Per-line pacing is the
# reliable middle ground: small enough to keep the test fast, slow
# enough to let x/vt track each line.)

for i in $(seq 1 10); do
  printf 'Line %s content\n' "$i"
  sleep 0.02
done
