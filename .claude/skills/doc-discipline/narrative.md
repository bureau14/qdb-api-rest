# Narrative comments in complex functions

Goal: a reader can understand a complex function by reading only its comments,
top to bottom, and then confirm each claim against the code directly below it.

## Which functions qualify

A function qualifies when explaining it to a colleague takes more than one
sentence. Typically it:

- captures business logic: rules a domain expert would recognize, policies,
  precedence, defaults;
- is an algorithm: several steps whose order matters, or a loop or data layout
  shaped by a performance concern;
- has non-obvious control flow: early exits, state machines, branches that
  "cannot happen";
- relies on an invariant established elsewhere.

Functions that do not qualify get no narrative: forwarders, accessors,
constructors that assign, glue whose name and body already say everything.
Leave them bare. Comment density should track complexity, not line count.

Decide in this order; the first rule that applies wins:

1. It forwards, accesses, assigns or converts in one obvious step: bare.
2. Its name plus its body already say everything a reader needs: bare.
3. It encodes a domain rule, is an algorithm, has non-obvious control flow, or
   depends on an invariant established elsewhere: narrate.
4. Anything else: bare.

Length alone never qualifies a function, and the default is bare.

INCORRECT, a forwarder narrated out of habit:

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

CORRECT, the same function:

```
filled(a) -> array
{
    return filled(a, null_value_of(a.type))
}
```

Every comment in the first version repeats what the line below it already
says. Removing them loses nothing, which is the test.

Record the decision for every function in scope as one row of the narrative
plan before changing anything:

| function (file:symbol) | qualifies | reason (rule 1-4) | why-evidence |
| ---------------------- | --------- | ----------------- | ------------ |

**why-evidence** lists, for each reason you intend to state in a comment, where
you verified it: a `file:line`, a test, a doc passage, a commit. A reason
without evidence is not written; the comment states the "what" instead and the
function goes under "Rationale unknown".

## The shape

**1. Doc comment above the function: the contract.** What it does and returns
for a caller, preconditions, what it mirrors or is compatible with. No
implementation detail. If an existing doc comment explains how the function
works, move that part inside the body.

**2. Overview comment at the top of the body: the process.** Before the first
statement of real work, describe how the function goes about its job and why
it is shaped this way: the strategy, the constraint that forced it, the fast
and slow paths. When the process is a sequence, write it as a numbered list.
Preconditions checked by assertions come before it, each with a line saying
what is being required.

**3. Step comments: one per step, as the code proceeds.** A single line where
it fits, several where it does not. When the overview is numbered, the step
comments carry the same numbers and wording, so the overview works as a table
of contents. Numbers must be unique and in order.

A step comment says what the step achieves or why it is done this way. It does
not paraphrase syntax.

| Instead of                | Write                                                  |
| ------------------------- | ------------------------------------------------------ |
| `// loop over the chunks` | `// Exit early once the mask is known to be mixed`     |
| `// allocate buffer`      | `// Worst case: every code point takes 4 bytes`        |
| `// check if null`        | `// Null counts as missing, not as a coercion failure` |

**4. Honesty markers, where they apply:**

- a constant or threshold says where it came from, including "not measured";
- a branch that should be impossible says why, and what it would mean if hit;
- a hot path or a deliberately odd construct says so and says what was
  rejected: `XXX(name): ...`;
- known shortcomings: `TODO(name): [topic] ...`;
- surprising side effects (allocation ownership, caching, I/O) get a `Note:`.

**5. Assertions are part of the narrative.** Where a comment claims an
invariant that can be checked cheaply, an assertion next to it is better than
the claim alone. Adding one changes code, so in this skill report it under
"Needs a human" rather than adding it.

## Example

Language-neutral pseudocode. The function decides whether one condition of a
rule matches a record.
It illustrates comment shape only; the semantics shown are not the
specification (the conformance fixtures are).

```
/// Whether condition `c` matches record `r`.
///
/// Never raises on bad data: what happens on a missing field or an
/// uncoercible value is decided by the policies carried on `c`.
condition_matches(c, r) -> bool
{
    // A condition is decided in three stages, and the order matters because
    // each stage has its own policy for giving up:
    //
    //  1. resolve the field path to zero or more candidate values;
    //  2. coerce each candidate to the declared field type;
    //  3. compare, where ANY candidate satisfying the operator is a match.
    //
    // Missing (stage 1) and uncoercible (stage 2) are different failures with
    // different policies. Null is treated as missing, never as uncoercible.

    ////
    // 1. resolve the field path
    //
    // Wildcards make this a set of values rather than one. An empty array
    // under a wildcard resolves to nothing, which counts as missing.
    candidates = resolve(c.path, r)

    if is_empty(candidates) or all_null(candidates)
    {
        return apply_policy(c.on_missing_field)
    }

    ////
    // 2. coerce each candidate, 3. compare
    //
    // Interleaved rather than two passes so that the first match can exit
    // without coercing the remaining candidates.
    any_coerced = false

    for v in candidates
    {
        // A null among non-null candidates is skipped, not failed: the other
        // elements can still decide the condition.
        if is_null(v) { continue }

        x = coerce(v, c.field_type)
        if is_failure(x) { continue }

        any_coerced = true

        // First match wins; evaluation order is unspecified, the result is not.
        if compare(c.op, x, c.value) { return true }
    }

    // Nothing matched. If that is because nothing could even be coerced, the
    // coercion policy decides; otherwise it is an ordinary non-match.
    return any_coerced ? false : apply_policy(c.on_coercion_fail)
}
```

Note what is absent: no comment on `continue`, none restating the signature,
none on the final `return` beyond the one reason it is not simply `false`.

## Applying it to existing code

1. Decide whether the function qualifies, using the ordered rules above. If
   it does not, remove only noise comments that paraphrase syntax, and stop.
   Commented-out code is reported under "Needs a human", not deleted.
2. Read the whole function and its callers' expectations before writing.
   Write the overview last, after the steps are understood.
3. Keep existing comments that are correct, and fold them into the shape. The
   author's wording, tags and names stay.
4. When the why cannot be verified, write the what, and list the function
   under "Rationale unknown". Do not guess at performance motives or history.
5. When the overview needs more than about seven steps, or the steps do not
   form one story, the function is a split candidate. Narrate it as it is and
   list it under "Needs a human"; small composable functions are this
   project's preference, but refactoring is outside this skill.
6. After writing a function's comments, read them back top to bottom as a
   list of separate claims and check each one against the code directly below
   it. Fix or delete any claim the code does not bear out. Check that step
   numbers are unique, in order, and match the overview.
7. New code written elsewhere in this project follows the same shape from the
   start; this skill is the repair path, not the primary one.
