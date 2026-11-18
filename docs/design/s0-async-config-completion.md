# S0 asynchronous update-hook completion — implementation record

Recorded before adding the common cell/request implementation for frozen D29.
This preserves checklist row 16's carried raw/effective/config/update/commit
semantics; it changes no frozen script, assertion, criterion or comparison rule.

The existing synchronous `ConfigHooks.Update` remains supported. An optional
`UpdateAsync` bridge returns an actual completion waiter after executing its
synchronous hook prefix. The cell's request method performs candidate resolution,
persistence, raw retention and failure clearing synchronously, then invokes that
prefix. It never moves the entire update operation into a goroutine.

`Configuration.RequestApplyUpdate` returns the completion waiter and any prefix
error. Existing `ApplyUpdate` delegates to the request and waits, preserving its
synchronous contract. Native `RequestUpdateConfig` returns an external Await that
joins hook completion and, only if the restart continuation actually ran, the
latest fiber transition. No mutable request closure is overwritten concurrently.

A veto publishes its resolved effective candidate only after the actual hook
continuation succeeds and the same raw-version, publication-generation and native
binding guards still hold. A rejected waiter retains raw input and old effective
configuration; a stale old completion cannot overwrite a newer publication.
Wait cancellation is not successful completion and cannot publish. Repeated
external waits must not rerun hook/next or publish twice. No callback runs under
the cell lock, graph lock or reconciler state owner.

The D29 fixture registers the real hook only during its activation's Apply. The
actual Waterfall engine dispatch/lease and native tracked work own suspended
continuations; prefix/barrier handshakes record actual observations, not expected
values. Errors and completions remain distinct from fiber lifecycle completion.
All five original D29 assertions and complete raw trace comparisons run unchanged;
unsupported continuations remain unsupported, never guessed or silently omitted.
