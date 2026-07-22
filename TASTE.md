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
