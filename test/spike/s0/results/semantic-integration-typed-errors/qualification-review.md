# Qualification review of this retained semantic run

The complete observed traces in this directory matched the selected hook
D31–D36 strict comparisons on both candidates; selected causal integration also
exited 0. Those observations are not retracted or relabelled as source failures.

However, precommit review found that the hook leaf receipt generator recorded a
selected **after-run on-disk** source list only. It did not capture a complete
before/after compiled closure or the Go test-executable hash, and its adaptation
text still described the earlier joined-string implementation. Therefore these
hook leaves alone are **unqualified as current-source acceptance evidence**.
Do not infer signed/attested build identity from the original receipt.

The original files and receipt remain unchanged. A separately named fresh run
must bind complete before/after source manifests, executable bytes, actual command,
exit/race log and accurate typed-error encoding before source-qualified hook
acceptance can be reported. Causal leaf receipts already capture their own
broader identity/fence and retain strict outcomes separately. Neither subset
establishes full corpus, final SDK, numerical budget or complete S0 acceptance.

Later review also identified native configuration publication and resource-ACK/
stale-context races. Their regressions and fixes require affected reruns against
a new source identity; earlier complete property passes remain historical only.
