---
name: rr-explain-plan
description: Explain the current unit's plan to the owner as a figure-first page -- the landscape the change lands in, drawn from the tree, the change drawn on it, and every constraint the plan rests on marked with its origin, so the owner sees what the plan implies and what existing code forces, and can overrule. The plan stage runs it once, after the plan commit and before the gate; the owner invokes it at any time after that. It is not a stage, opens no gate and changes nothing in the tree.
disable-model-invocation: true
argument-hint: [docs/<slug>-plan.md]
---

# /rr-explain-plan -- the plan explained

Run `/rr-workflow` first. This skill is not one of its stages. It
opens no gate, writes nothing under the repository, commits nothing
and never edits the plan. The plan stage runs it once, between the
plan commit and the gate, so the owner reads the page before deciding
on the plan. The owner invokes it again at any time, before, during or
after the plannotator gate on it. What the owner learns travels into
the gate as an annotation; it never travels through this skill.

## Entry check

Current branch: !`git branch --show-current`

Plan documents on this branch (empty means none):

!`git ls-tree --name-only HEAD docs/ | grep -- '-plan.md' || true`

Plan given as argument (empty means infer):

<plan-path>
$ARGUMENTS
</plan-path>

The plan is the argument when one is given, else the one plan the
list shows, else `docs/<slug>-plan.md` for the branch's
`sc-19567/rr-<slug>`. No plan, or two with no argument, is a stop:
say which, and the owner names one.

## What the page is for

The owner reads the plan and sees what it chooses. The owner cannot
see from the plan alone what forced each choice, because the forcing
things are in the tree, in the vendored dependencies and in earlier
decisions, and the plan cites them as paths. The page draws the
forcing things. Every design choice in the plan rests on a constraint,
and every constraint has one of four origins:

1. `owner`: a decision the owner made, a Rationale row settled by
   "owner, <date>", an accepted ADR's Decision, a brief section the
   brief names as a pre-project decision.
2. `dependency`: a rule of code this repository does not own: a
   vendored package, the daemon, the wire protocol.
3. `existing code`: the shape of code in this repository that the
   plan works around, builds on or tears out: an import edge, an
   ownership rule, a split that assumed something, a helper written
   for one caller.
4. `convention`: a rule of an `AGENTS.md`, a document or an ADR's
   consequences that the plan follows.

The origins differ in what the owner can do about them. A dependency
rule cannot be changed here. An owner decision was the owner's and
stands until the owner changes it. A convention is revised cheaply. An
existing-code constraint is the one the owner may want gone: the plan
may have accepted a shape that was already wrong, and the owner
decides that only when the page shows the shape, where it comes from
and what the plan does because of it. So every constraint on the page
carries its origin in its form, and the existing-code origin is the
most visible of the four.

The page explains and never reviews. It draws no verdict, recommends
nothing and says nothing about the plan's quality. It may mark a thing
as important, as `page.md` prescribes, and the owner judges.

## Step by step

1. Read the plan whole. From its Design section, list the files and
   packages it touches; from its Verified facts, list the paths it
   cites; from its Rationale, list the rows. This is the scoped set.
2. Read the tree for the scoped set: the `AGENTS.md` of every touched
   folder and of each parent on the way to the root, the touched
   packages top to bottom with their call sites, and the vendored or
   daemon code the facts cite, at the lines cited. The landscape is
   drawn from what you read here, never from the plan's description
   of it. A component you did not read is not drawn.
3. Build the constraint table, in the conversation, before any
   drawing:

   ```
   | constraint | origin | evidence (path:line) | what the plan does because of it | plan vs tree |
   ```

   One row per thing that forces a choice in the plan. "Origin" is
   one of the four above. "Evidence" is where the tree or a document
   shows it, read in step 2; a row with no evidence is deleted, never
   kept on the plan's word. "Plan vs tree" is `agrees`, or `differs:
<what the tree shows>` when a plan claim about existing code does
   not match what you read. A difference is drawn on the page as two
   labels on the same element, the plan's and the tree's, and is not
   judged.

4. The scoping round. First, post the commit overview in the chat:
   one line per build commit of the plan's Commits section, in
   landing order, saying in plain words what the commit does, which
   files it touches and how many functions it adds, changes or
   removes, read from the Design section. Group the lines under at
   most four headings, each the Design section or package the
   commits change, because the question that follows offers one
   option per group. The doc-discipline runs, the deletion commit
   and the Verify step are not listed.

   Then, through the question tool, one call, four questions, each
   worded by the rules of the seed's challenge round (context before
   the question, an identifier only after the sentence that explains
   it, options in plain words, three to six sentences):
   - which commit groups get a commit card of their own,
     multi-select, one option per group of the overview; the owner
     answers "Other" with commit numbers to pick inside a group. The
     card is `page.md`'s section 4: the one mechanism the commit
     changes drawn large, before and after, with its ledger and the
     plan's sketch of its main function;
   - which of the touched components the owner already knows well,
     multi-select, options from the Design section's file headings
     grouped by package; a known component is drawn as one labeled
     box with no interior;
   - which decisions the owner wants traced through their
     consequences, multi-select, options from the Rationale rows;
     a traced decision gets its own figure showing what it forces
     elsewhere in the tree and what the rejected alternative would
     have forced instead;
   - which concerns the owner wants to see through this time,
     multi-select, options you derive from the plan: the kinds of
     thing it changes, such as batch ownership, a wire shape, an
     error precedence, a refactor of existing code, the tests. More
     than one is the normal answer.

   What the owner does not answer takes the default: a card for
   every commit that adds, changes or removes more than one function,
   every component drawn whole, every decision at one depth, every
   concern covered.
   An answer is scope, not instruction: it changes what is drawn,
   never what the plan says.

5. Read, in this order: `page.md` in this folder; the
   `plannotator-visual-explainer` skill's `SKILL.md` and its
   `references/theme-override.md`; the `visual-explainer` skill's
   `SKILL.md`, `references/style-guide.md`, `references/diagrams.md`
   and `references/motion.md`. Then write the page in the shape
   `page.md` fixes, as `~/.agent/diagrams/rr-<slug>-explain.src.html`,
   and render it with the kit's renderer without `--open`:

   ```
   node ~/.claude/skills/visual-explainer/kit/render.mjs ~/.agent/diagrams/rr-<slug>-explain.src.html
   ```

   Fix every finding the renderer prints and render again until it
   prints none. Nothing of this lands under the repository and no
   document links to the page.

6. Deliver through the annotation tool, never by opening a browser
   yourself:

   ```
   plannotator annotate ~/.agent/diagrams/rr-<slug>-explain.html
   ```

   Start it in the background with a long timeout, never in the
   foreground and backgrounded later, because a run moved to the
   background mid-stream loses the annotations already written. It
   blocks until the owner closes it, and the harness wakes you when
   it exits. Do nothing while it runs. One plannotator session at a
   time: a plan gate open in the background while the explainer's
   session opens comes back `dismissed`, so when the gate is running,
   say so and the owner decides which runs first.

7. The loop. Each annotation names a part of the page and says what
   the owner wants explained better or differently. Revise the
   section the annotation points at, in the source, with small
   edits; a figure the owner asks to go deeper on gets the next
   level of detail from the tree, read now if not read in step 2.
   Render, and deliver again from step 6. An annotation that asks for
   a change to the plan or to code is answered in the chat with one
   line: it belongs in the gate, or in the stage the owner opens, and
   the page does not carry it. A session that closes with no
   annotation ends the loop.
8. Close with one line: the page's path and the count of hints in
   its closing list.

## Never

- Edit the plan, a document, code or a test, or commit anything.
  The explainer reads the tree and writes one page outside it.
- Draw a component from the plan's description of it. The plan says
  what will be; the tree says what is; the landscape is the tree's.
- Invent a reason. A constraint whose origin you cannot evidence is
  not on the page; a plan claim you cannot place is a difference,
  drawn as such.
- Judge. No verdict on a decision, no "should", no better or worse.
  A hint says a thing matters and why the owner would look; the
  owner decides what to do.
- Answer the scoping round yourself, or skip it because the plan is
  small.

Correct: the plan moves a constant into `internal/model`; the table
has a row "`encoding` and `qdb` import `model` and not each other",
origin `existing code`, evidence the two packages' non-test imports,
and the landscape draws the three packages with the two import edges
and the constant's new box on `model`; the hint marker on that edge
says the import graph is what put the constant there. The owner reads
it and annotates "is model the right home or is this the import graph
being wrong"; that is the owner's to take into the gate, and the page
only showed the edge.

Incorrect: the landscape draws `query.Run` from the plan's sentence
about it, and the tree's `query.Run` takes an option the plan never
mentions. Or: a Rationale row says "the owner wants every consumer to
read batches of one shape", and the page marks it `existing code`
because the batch helpers exist, so the owner's own decision looks
like something to refactor. Or: the closing list says the per-batch
ctx check "is the right call". Or: an annotation says "drop the
RetryInfo detail", and the explainer edits the plan.
