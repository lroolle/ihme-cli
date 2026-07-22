# TASTE — design scars

Prior design rulings for ihme-cli. One entry per real rejection, each
with its why — a rule without a why fossilizes into style police.
Read this before any design verdict; delete a scar when its expiry
condition arrives.

The test behind all of them: a surface that gets prettier while the
task gets harder is a costume, and it fails no matter how clean it
looks in a screenshot.

---

## 2026-07-21 rejected: the consent card without the verdict

**Why.** The first shipped agent consent card asked the user to
approve `turbo3_placard@icloud.com` with only the address, the label,
and a sentence restating the title ("This creates a new address in
your iCloud account."). The taste rationale — which the model is
*required* to write into the reserve call, and which is the product's
whole differentiator — appeared only *after* approval, in the ✓ step.
The user was deciding blind at the exact moment the information
existed and was withheld. Tidy card, hollow decision: a costume.

**Reuse.** A consent prompt is a decision surface, not a speed bump.
Whatever the agent knows that the decision depends on — the why, the
alternatives it rejected, the thing it will act on — belongs ON the
card, with the subject prominent and boilerplate cut. Corollary: if
the agent cannot state its why yet, it does not get to ask the user
at all — bounce the call back to the model instead of interrupting a
human with an unanswerable question.

**Expires.** Not expected to — this is what consent *is*. Revisit
only if a consent class appears whose why is genuinely self-evident
from the subject alone.

---

## 2026-07-22 rejected: a consent prompt on refresh_candidates

**Why.** `refresh_candidates` (reserve then delete a throwaway to force
Apple to hand out a fresh candidate pool) was first shipped behind a
light consent card, on the reflex that "every Apple mutation asks."
But refresh is a *net-zero transient*: it leaves no address behind.
Gating it added a prompt that reads as careful and consistent while
making the task harder — the exact friction that made the agent feel
dumb in the trace that motivated the feature — and it bought no
protection, because the real abuse bound is the per-task refresh cap
in code, not the prompt. Worse, every low-stakes confirmation spends
down the credibility of the *one* card that carries weight (the keeper
reservation, protected by the scar above). A confirmation that guards
nothing is a costume.

**Reuse.** Gate consent on PERSISTENT state the user will later see —
a new address, a deactivation, an edit. Do NOT gate net-zero
transients or local-only writes (refresh, memory recall/remember):
allow them and make them loud in the step log instead. Honest
feedback (invariant #8) is how the user stays informed; a
confirmation is not. The protection against churn is a code cap, not
a human tap.

**Expires.** If refresh ever stops being net-zero — e.g. it starts
leaving a persistent address behind by design rather than as a rare
delete-failure fallback — it rejoins the gated class.
