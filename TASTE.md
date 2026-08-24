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

**SUPERSEDED 2026-08-12** — the expiry fired in the field, just not
the one predicted: see the next entry.

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

---

## 2026-08-12 reversed: refresh_candidates rejoins the gated class

**Why.** The 2026-07-22 scar above assumed the per-task cap bounded
abuse and a consent card bought nothing. A field trace (deepseek via
the embedded agent) broke both legs: a model with a miscalibrated
taste bar declared a healthy pool "all three candidates are weak" and
burned throwaways task after task. (1) "Net-zero" described ACCOUNT
state, not API pressure — each refresh is a real reserve + delete
against Apple's HME endpoints, exactly the churn that draws rate
limiting or a ban. (2) The cap is per-task and resets every turn
(resetTurn), so capped-in-code still allowed steady burn across a
session. The consent card also fixes the upstream failure: it puts
the model's "every candidate fails because…" verdict in front of the
user, who can veto a bogus weak-pool call with one keystroke,
redirect ("just take sterner.turning5r"), or grant "a" for the
session. Shipped alongside a taste recalibration in SKILL.md (taste
ranks a pool, it rarely vetoes one) so the gate is a backstop, not
the fix.

**Reuse.** "Net-zero on persistent state" is not net-zero — remote
API cost is state the user pays for too. A per-task rate cap bounds
one task, never a session. And when a safeguard exists to catch model
misjudgment, the card must show the judgment being consented to.

**Expires.** If Apple ships an official pool-refresh endpoint (no
reserve+delete cost), or telemetry shows bogus weak-pool verdicts
have become rare across providers, refresh may argue its way back to
the ungated class.

---

## 2026-08-13 rejected: "the image" as the required rationale

**Why.** The 2026-08-12 recalibration fixed the gate (taste ranks,
rarely vetoes) but left the rationale schema demanding an image: the
system prompt required "the image it makes, the inspiration," the
summary had to restate "the image that made it win," and SKILL.md
step 4 asked for "the winner's image and why it fits." A field trace
(claude signup, pool `turbine.dives0s` / `navy-cliched9f` /
`fiends-salty-4j`) showed the new failure mode: the honest verdict
was "the only candidate without a defect word," but the model —
obligated to produce an image — confabulated one ("a power generator
plunging into deep water, energy meets depth"), force-fit service
resonance ("actually echoes what Claude does"), and delivered the
same purple paragraph twice, once narrating and once on the card.
The old bug vetoed pools for lacking poetry; the schema then made
poetry mandatory, so the model performed it. Fixed by making the
rationale one honest sentence in plain register, with image and
resonance allowed only when genuinely present, and by banning
pre-card restatement.

**Reuse.** A required field is an order to fill it: if the schema
demands an image, the model will manufacture one whether or not it
exists. Make the honest low-key answer ("cleanest of the three")
explicitly sufficient, or the model will decorate. Selling the pick
is a taste defect in its own right — resonance is discovered, not
constructed.

**Expires.** If a future rubric scores images separately from the
pass/fail verdict (image as scored bonus, not rationale), the
rationale field may reference it again.
