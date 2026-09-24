# Knowledge placement

Goal: every piece of knowledge lives once, at the most local place that still
covers everything it is about, and a reader can find it without knowing it
exists.

## The ladder

Place knowledge on the lowest rung that covers its subject. Lower wins.

| The knowledge is about ...                    | It lives in ...                                         |
| --------------------------------------------- | ------------------------------------------------------- |
| one expression, branch, constant or step      | an inline comment at that line                          |
| how one function goes about its job           | the overview comment at the top of that function's body |
| what one function promises to callers         | the doc comment above that function                     |
| one type, or one file as a whole              | the comment on the type, or the file header comment     |
| several files in one directory                | that directory's `AGENTS.md` (Invariants, Why, How to)  |
| several components                            | the documentation folder, per its `AGENTS.md`           |
| what something looked like before, or history | nowhere. Delete it; git keeps history                   |

Executable forms beat all prose rungs: if an assertion, a type, a schema or a
test fixture can state it, prefer that and keep at most one line of prose
saying why.

## Finding misplaced knowledge

Walk the documentation top-down and ask of every paragraph, table row and
bullet: "what is the smallest piece of code this is about?"

- The documentation folder: whatever its `AGENTS.md` calls misplaced, by its
  own rules (a routing table, an eviction rule, a budget). Typically sections
  describing a component's internals once that component has a directory, and
  decisions whose code home now exists.
- `AGENTS.md` files: anything about a single file or function (algorithm
  descriptions, field-by-field explanations, "function X does Y because Z",
  edge-case lists). Map rows are fine; essays about a row's subject are not.
- README-style files, commit-message-like notes, leftover plan or TODO files.
- File header comments that explain one function in that file.
- Doc comments above a function that explain implementation rather than
  contract. Implementation notes move inside the body.

Then walk the code bottom-up for the reverse problem, which is rarer:
comments that state cross-file or cross-component facts (protocol rules,
shared invariants, vocabulary definitions). Those move up to the `AGENTS.md`
or `doc/` rung, and the comment becomes a one-line pointer.

Record every finding as one row of the placement plan before changing
anything:

| passage (file:lines) | is about | rung | action | verified against |
| -------------------- | -------- | ---- | ------ | ---------------- |

- **is about**: the smallest piece of code the passage concerns.
- **rung**: where the ladder says it belongs.
- **action**: `move to <file:symbol>`, `pointer`, `delete (duplicate of ...)`,
  `delete (history)`, `keep`, or `report (contradicts code)`.
- **verified against**: the `file:line` that confirms the passage is true of
  the code today. Empty means unverified, and unverified passages are not
  moved; their action becomes `report`.

## Moving

1. Rewrite for the destination. A paragraph from a design document does not
   become a good comment by pasting it. Keep the facts and the reasons, drop
   the framing, and phrase it as a statement about the code directly below it.
2. Verify against the code while moving. Documentation that describes
   behavior the code does not have is not moved; it is reported under "Needs a
   human" with both versions quoted, because either could be the wrong one.
3. Delete the source passage. If the source needs to keep the topic visible,
   leave a pointer of one line at most: what, and which file.
4. If the destination function then qualifies as complex, shape the result per
   `narrative.md` even in `placement` mode. Do not leave a wall of text above
   a function.
5. After writing the destination comment, read it back as a list of separate
   claims and check each one against the code directly below it. Fix or
   delete any claim the code does not bear out.

## Discoverability

Knowledge in a comment is only useful if a reader who does not know it exists
can get to it. After every move, check all three:

1. **Reachable by walking.** Starting at the root `AGENTS.md` and following
   Map tables down, the reader arrives at the file. Every directory on the way
   has an `AGENTS.md`, every `AGENTS.md` is listed in its parent's Map, and
   the file has a Map row.
2. **The Map row advertises it.** The row's "when to read" cell names the
   topic in the words a reader would use ("how missing fields are handled",
   not "evaluation helpers"). Update the cell when knowledge moves in. One
   short phrase per topic; the row is an index entry, not a summary.
3. **Findable by search.** The comment uses the project vocabulary, as the
   documentation folder defines it, for the concepts it touches, spelled the
   same way, so a text search for the term lands on it. If the code uses a
   different identifier than the vocabulary term, mention the term once.

Also repair what you pass on the way: `AGENTS.md` files missing from their
parent's Map, Map rows pointing at files that no longer exist, files with
notable logic and no Map row.

## Budgets and checks

While here, run every check the documentation folder's `AGENTS.md` lists
(budgets, greps for words or numbers it forbids) and confirm the section
order it prescribes for component `AGENTS.md` files. Overruns are fixed by
moving knowledge down the ladder, never by splitting a document into more
documents. What cannot be fixed by moving goes in the report.
