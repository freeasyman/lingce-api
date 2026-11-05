# Deal card compiler v2

`Compile(snapshot, intermediate, RulesVersion)` is a pure compiler. Both inputs
are JSON bytes. The result contains `status`, canonical server-owned card bytes
and deterministic path diagnostics. `SnapshotID(payload)` seals input JSON
without converting identifiers through floating point. `ValidateCard` checks the
final schema, evidence graph and server-owned business invariants.

The compiler is deliberately not wired into the old analyzer. Phase 3 will own
immutable invocation bundles, snapshots, run storage and API integration. The
final contract is `deal-card-v2`; callers must not feed it to v1.1 consumers
without adapting their status and evidence handling.

Evidence IDs follow usable source order in the frozen snapshot. IDs do not
change when optional findings are removed; unused IDs can leave gaps. Only
evidence referenced by the final card is emitted. Quotes are exact complete
source text, and unknown timestamps remain null. `used_for` is derived from all
retained references, including nested conditions, actions and quality findings.

Business events and present intent are separate. Only matching verified events
can give an outcome state a `verified_fact` basis; spoken events remain
`reported_only`. A historic payment does not erase a present deferral/rejection.
Refunds require review because the input has no amounts or supersession links.
Verified payment/deposit blocks a new sales action; service or review handling
must be decided separately. Event-triggered time windows contain no fabricated
execution date. A system-suggested window is explicitly separate from a customer
source. Partial coverage conservatively withholds quality scores because this
intermediate protocol cannot distinguish absence-based scoring from observed
behavior. All these choices are recorded in the phase 2 report.

The final summary, verification and brief are server-generated after policy and
action filtering. A review/contact restriction cannot be undone by model prose.
Other free-text judgments still require the phase 4/5 semantic evaluation;
checking that a source exists is not proof that it supports the judgment.

Run from the API repository:

```sh
GOCACHE=/private/tmp/lingce-gocache go test ./internal/dealexpert/compiler -count=1 -v
GOCACHE=/private/tmp/lingce-gocache go test -race ./internal/dealexpert/compiler -count=1
GOCACHE=/private/tmp/lingce-gocache go test ./internal/dealexpert/... -count=1
GOCACHE=/private/tmp/lingce-gocache go vet ./internal/dealexpert/compiler
GOCACHE=/private/tmp/lingce-gocache go build ./internal/dealexpert/...
```

Input/intermediate schemas and all test fixtures are copied from phase 1 and
contain synthetic data only. `generate_schema.py` generates the v2 final schema
from the existing display body; the checked-in final schema is the runtime
source. Its generation inputs will join the immutable version bundle in phase 3.
