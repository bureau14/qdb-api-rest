# Comments inside functions

Goal: a colleague can change a function safely after reading its doc comment
and the few comments at the lines that need them. The comments are short,
they sit where the question arises, and they describe the code as it is
today.

The rules are "Code comments" and "Prose" in the root `AGENTS.md`. This file
says how to apply them and shows examples. The examples are illustrations of
shape. Do not copy their wording or their semantics.

## Which level a function gets

Decide in this order. The first rule that applies wins.

1. **Bare.** The function forwards, accesses, assigns or converts in one
   obvious step, or its name and body already say everything a reader needs.
   It gets no comments inside its body, and a doc comment only where the
   language's convention for public items wants one.
2. **Overview and inline.** The function's steps must run in an order the
   code does not make obvious, or it is an algorithm whose strategy no single
   line shows. It gets a doc comment, an overview of one numbered line per
   step, and an inline reason at each step or line that needs one.
3. **Inline.** The function has at least one line a colleague would stop at
   and ask "why?". It gets a doc comment and one inline comment at each such
   line. This is the common case, and it includes handlers, middleware and
   tests.
4. **Bare**, for anything else.

Branch count and length never qualify a function. A function with five
obvious branches is bare, and a function with one surprising line gets one
comment.

Record the decision for every function in scope as one row of the plan before
changing anything:

| function (file:symbol) | level | lines that need a why | why-evidence |
| ---------------------- | ----- | --------------------- | ------------ |

**why-evidence** lists, for each reason you intend to write, where you
verified it: a `file:line`, a test, a doc passage or a commit. A reason
without evidence is not written. The function goes under "Rationale unknown"
instead.

## Budgets

- A doc comment is one to three sentences of contract.
- An inline comment is one line, two at most.
- An overview is one line per step and carries no reasons.
- Comments stay well under half of a function body's lines.

A comment that needs more room is usually explaining several things. Split
it, and move each part to the line it explains.

## Examples

### A forwarder

INCORRECT:

```
/// Fills masked values using the default null value.
filled(a) -> array
{
    // Get the null value for this type
    v = null_value_of(a.type)

    // Call filled with the null value
    return filled(a, v)
}
```

CORRECT:

```
filled(a) -> array
{
    return filled(a, null_value_of(a.type))
}
```

Every comment in the first version repeats the line below it.

### An algorithm, in pseudocode

The function decides whether one condition of a rule matches a record. Its
steps must run in order, so it gets an overview.

```
/// Whether condition `c` matches record `r`. Never raises on bad data: the
/// policies on `c` decide missing fields and uncoercible values.
condition_matches(c, r) -> bool
{
    // 1. resolve the field path to candidate values
    // 2. coerce each candidate to the field type
    // 3. compare, matching when any candidate satisfies the operator

    // 1. A wildcard over an empty array resolves to nothing, which counts
    //    as missing.
    candidates = resolve(c.path, r)
    if is_empty(candidates) or all_null(candidates)
    {
        return apply_policy(c.on_missing_field)
    }

    // 2. and 3. share one loop, so the first match skips the remaining
    //    coercions.
    any_coerced = false
    for v in candidates
    {
        // A null or uncoercible candidate is skipped, because the others
        // can still decide the condition.
        if is_null(v) { continue }
        x = coerce(v, c.field_type)
        if is_failure(x) { continue }
        any_coerced = true
        if compare(c.op, x, c.value) { return true }
    }

    // When no candidate coerced, the coercion policy decides.
    return any_coerced ? false : apply_policy(c.on_coercion_fail)
}
```

The overview lists the steps and gives no reasons. Each reason sits at the
line it explains. Nothing comments the `continue`, the signature or the
loop.

### A doc comment that explains the implementation, in Go

INCORRECT:

```go
// Call runs f on a session authenticated as the user. The breaker gates
// the call. The user's pool leases a session, dialing one on demand, and
// runs f. The binding's IsBadSession and Lease.Done decide from f's error
// whether the pool keeps the session. This layer adds two decisions, both
// per cluster. Only errors that are evidence about the cluster
// (IsClusterUnavailable) feed the breaker. An idempotent read can opt in
// to one retry after a retryable failure, and a refused credential is
// never retried. A retry after a bad session runs on a fresh session,
// because the pool discarded the old one. A retry after any other
// retryable failure may run on the same session.
func (c *Cluster) Call(ctx context.Context, u User, f func(*Session) error, opts ...CallOption) error {
```

CORRECT:

```go
// Call runs f on a session authenticated as u. It returns a
// *BreakerOpenError while the cluster is unreachable. WithReadRetry allows
// one retry of an idempotent read.
func (c *Cluster) Call(ctx context.Context, u User, f func(*Session) error, opts ...CallOption) error {
	...
		// The caller's own cancellation says nothing about the cluster.
		if callerLeft(err) {
			return err
		}
		c.feedBreaker(err)
		// A second dial with refused credentials is refused the same way.
		if !cc.retry || !qdbapi.IsRetryable(err) || errors.Is(err, ErrCredentialsRefused) {
```

The first version tells callers how the pool and the breaker work, and each
change to `Call` added a clause to it. The second version gives callers the
contract and puts each reason at the line it explains. The facts about the
pool belong to the pool's own comments.

### A change, in Rust

The code before the change:

```rust
/// Readiness: the instance may receive traffic. `ok` as soon as the
/// listener is up.
async fn readyz() -> &'static str {
    // TODO(store): probe the store.
    "ok"
}
```

The change adds a store probe. INCORRECT, the old comment patched:

```rust
/// Readiness: the instance may receive traffic. `ok` as soon as the
/// listener is up, and now also once the store answers.
async fn readyz(State(s): State<Store>) -> StatusCode {
    // The store probe replaces the earlier unconditional `ok`.
    s.ping().await.map_or(StatusCode::SERVICE_UNAVAILABLE, |()| StatusCode::OK)
}
```

CORRECT, the comment written from the new code:

```rust
/// Readiness: the instance may receive traffic. 200 once the store answers
/// a ping, 503 while it does not.
async fn readyz(State(s): State<Store>) -> StatusCode {
    s.ping().await.map_or(StatusCode::SERVICE_UNAVAILABLE, |()| StatusCode::OK)
}
```

The patched version keeps the sentence about the listener, adds "now", and
explains the history in the body. The rewritten version reads as if the
function had always probed the store.

## Applying it to existing code

1. Decide the level with the ordered rules above. A bare function loses its
   noise comments, and nothing else changes. Commented-out code is reported
   under "Needs a human" and stays in place.
2. Read the whole function and what its callers expect before writing.
3. Write the function's comments from the code, before you reread the
   existing ones. Then compare with the existing comments and carry over
   only what the code cannot show: a verified reason, a `TODO(name)` or
   `XXX(name)` tag, a name a reader searches for. Existing wording does not
   survive because it is there. A comment that reads as a patch on an
   earlier version is rewritten whole, even when every clause is true.
4. When the why cannot be verified, write the what, and list the function
   under "Rationale unknown". Do not guess at performance motives or
   history.
5. When the overview needs more than about seven steps, or the steps do not
   form one sequence, the function is a split candidate. Comment it as it is
   and list it under "Needs a human".
6. Read the comments back in three passes, in this order:
   1. **Truth.** Read each comment as a separate claim and check it against
      the code directly below it. Fix or delete any claim the code does not
      bear out. Check that the step numbers are unique, in order, and match
      the overview.
   2. **Cut.** Delete or rewrite every comment that does one of the
      following:
      - repeats what the line below it says;
      - repeats another comment, including the overview;
      - mentions an earlier version, a plan, a review, an issue or the
        conversation;
      - explains implementation in a doc comment;
      - names a rejected alternative that a reader of that line would not
        propose;
      - reads like one of the "Written" sentences in the root `AGENTS.md`,
        "Prose".
   3. **Gaps.** Cover the code and read the comments alone. Ask one
      question: could a colleague change this function safely? Each line
      they would have to puzzle over is a missing inline comment. Write it
      from its evidence, or ask the owner. Never guess.
7. New code elsewhere in this project follows the same rules from the start.
   This skill is the repair path.
