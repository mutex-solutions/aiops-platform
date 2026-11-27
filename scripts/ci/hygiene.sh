#!/usr/bin/env bash
# Fails when tracked files carry machine-local or private details.
# HYGIENE_DENYLIST (optional, extended regex) adds project-private terms
# without writing them into the repository.
set -euo pipefail

pattern='(/home/[a-z][a-z0-9_-]+/|/Users/[A-Za-z]+/|\b(10\.[0-9]+|192\.168|172\.(1[6-9]|2[0-9]|3[01]))\.[0-9]+\.[0-9]+\b)'
status=0

if git grep -nIE "$pattern" -- . ':!scripts/ci/hygiene.sh'; then
  echo "error: local paths or private addresses found above" >&2
  status=1
fi

# Built binaries never enter the repository: a Go binary carries the paths of the
# machine that built it. Refuse any tracked executable (ELF, Mach-O, PE) and any
# tracked file over 4 MiB.
if ! git ls-files -z | perl -0ne '
  chomp; next unless -f $_;
  if (-s $_ > 4194304) { print STDERR "error: tracked file over 4 MiB: $_\n"; $bad = 1 }
  open(my $fh, "<:raw", $_) or next; read($fh, my $m, 4); close $fh;
  if ($m =~ /\A(\x7fELF|\xcf\xfa\xed\xfe|\xce\xfa\xed\xfe|\xfe\xed\xfa[\xce\xcf]|MZ)/) {
    print STDERR "error: tracked executable: $_\n"; $bad = 1 }
  END { exit($bad ? 1 : 0) }'; then
  status=1
fi

if [ -n "${HYGIENE_DENYLIST:-}" ]; then
  # Print file names only, so the private terms never reach the log.
  if git grep -lIiE "$HYGIENE_DENYLIST" -- . >/tmp/hygiene-denied.txt; then
    echo "error: denylisted terms found in:" >&2
    cat /tmp/hygiene-denied.txt >&2
    status=1
  fi
fi

exit "$status"
