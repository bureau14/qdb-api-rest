# Shape the unit

The report explains the unit to the owner before it lists anything,
so settle its shape before writing. From what you read, decide:

1. The outcome: what is true in the tree when the unit is done, and
   what it leaves for later.
2. The approach: the shape of the change, where it lives, what it
   reuses, what it introduces.
3. Each design choice the unit requires: the option you take, the
   alternative it beats, the reason, and what settles it. A choice an
   `owner` source settles carries that path. A choice only `agent`
   sources settle, or that the sources leave open, goes to the owner in
   the challenge round below: what the owner answers is settled by the
   owner, and what the owner leaves to you is a proposal: you still
   pick, you say the pick is yours, and the plan's approval is what
   settles it. A choice that contradicts a locked decision, or that
   would make the unit materially different work, is not yours to make;
   it goes under Open questions.

# Challenge round

The plan is written after this report, and it carries the approach the
owner wants, not the approach the documents happen to record. So before
the report, put the choices to the owner through the question tool
(AskUserQuestion), while the context is loaded and nothing is written.

What gets a question. A choice the unit's code rests on that is:

1. `agent` by provenance, including every detail of a future spec the
   unit would be the first to implement;
2. drift between sources, or between a source and the tree;
3. expensive to reverse once code exists: a wire shape, a public
   signature, a config key, a storage layout, a dependency;
4. one where a simpler alternative exists and no source records why it
   lost.

What gets no question: a choice marked `owner` with a date; a choice
the tree settles; a choice that belongs to a later unit or milestone; a
choice between wordings. A question whose answer would not change a
commit of this unit is not asked.

How to ask. One question per choice, at most four per call, at most
two calls; what remains goes under Open questions in the report, and
the owner answers it in the conversation before opening the plan stage.

The owner reads the question cold: the context you loaded is not in
front of them, and a name from the code means nothing until the
question says what it is. So the question text carries the context,
in this order, before it asks anything: what the thing is and what it
does today, in plain words ("the server signs a token at login; the
token carries who you are and when it expires"); what the unit changes
about it and why that raises a choice ("login will now answer two
tokens, and the code that signs one has to produce the pair"); then
the question. An identifier appears only after the sentence that
explains it, in parentheses, so the owner can find it in the code. The
question runs three to six sentences; a one-line question is a sign
the context was left out.

The options follow the same rule. The label says the choice in plain
words, not in identifiers ("one signing path for login and refresh",
not "MintPair(user, secret) over pair(base)"). The description says
what the option does, then what it costs and what it keeps open, and
names an identifier only after saying what it is. The options are: the
approach the sources record, named by its path; the simplest
alternative; a third where a real one exists. Put the option you would
take first, marked "(Recommended)", and say in one clause why. The
owner can always answer "Other" with text. An answer is the decision;
it is not argued with.

Where the answers go. Each answer is the owner's decision as of today's
date: in the report's Decisions entry as `settled by owner, <date>`,
and in the plan's Rationale row under "settled by" in the same words.
A choice the owner left to you is `proposal` and keeps your
recommendation. An answer that gives the unit a new direction reshapes
it before the report is written: run Shape the unit again for what the
answer touches.

Correct: the brief's endpoint sketch gives the response a `rows` array
of arrays, no document says why, and the unit adds the first handler
that answers it. The question says: the query endpoint answers a
table; today nothing produces it; this unit adds the first handler, so
the shape every later client parses is fixed now; the brief sketches
each row as an array of values in column order; should rows be arrays
or objects keyed by column name? The options are "arrays in column
order (the brief's sketch)" and "objects keyed by column", each with
what it costs. The owner picks arrays; the Decisions entry says
`settled by owner, <date>`, and the plan's Rationale repeats it.

Incorrect: the sketch is followed because it is in the brief, and the
owner learns the shape from the diff. Or: the question is "How should
Tokens mint the login's pair?" and the options are signatures, so the
owner has to open the package to learn what minting is and what the
pair is before answering. Or: eight questions go out in one call, three
of them about the milestone after this one. Or: the owner picks objects
and the plan says arrays "as the brief prescribes".
