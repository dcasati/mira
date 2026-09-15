---
name: radio-communications
description: >-
  Draft and review clear, concise radio and push-to-talk (PTT) messages, or write
  radio-assistant instructions using Canada's ISED RIC-22 as a procedural baseline.
  Use this skill whenever the user asks for radio wording, dispatcher-style replies,
  PTT etiquette, call signs, phonetics, prowords, radio read-back, or RIC-22 guidance,
  even without naming the skill. Includes text-only communication guidance for
  Zello-style PTT, not a claim that RIC-22 legally governs that platform.
  Not for broadcast content, RF engineering, licensing decisions, tactical procedures,
  live emergency simulation, speech generation, transmission, cloud calls, deployment,
  or changing live assistant policies.
---

# Radio Communications

Produce text-only drafts, reviews, and proposed assistant instructions. Use clear
ordinary language plus established procedural words; preserve meaning before brevity.

## Scope and authority

- **Official baseline:** ISED's *RIC-22 — General Radio Operating Procedures*,
  Issue 4, January 2008; accessed **2026-09-15**. Its preface says circulars
  provide guidance and have no status in law. It is useful to all radio operators,
  not a police-specific standard or a licence to operate.
- **Local preference, not RIC-22:** aim for **20 words or fewer** in routine
  assistant responses. No greeting, filler, unsolicited advice, follow-up offer,
  or unnecessary question. Provide extra detail when explicitly requested.
- **Exceptions:** retain all necessary identification, uncertainty, safety information,
  units, time zones, and required qualifications even when exceeding 20 words.
  Never truncate a warning or omit a material fact to meet the target.
- Apply applicable law, actual authorizations, and approved service/network SOPs.
  If a supplied SOP differs from RIC-22, identify the difference rather than
  claiming equivalence. Do not invent or silently import police jargon or 10-codes.
- RIC-22's emergency procedures are primarily aeronautical/maritime; it notes their
  rare use in land-mobile service. Do not transplant those procedures into an
  unspecified network, treat every hazard as a formal safety signal, or teach tactics.

## Prerequisites and tool boundaries

| Task | Prerequisites and limits |
|---|---|
| Draft, review, explain, or propose instructions | No MCP server, authentication, VPN, account, or radio connection required. Use the supplied facts and on-demand local references. |
| Verify a changed source | Public official ISED source access only, when necessary. Never upload operational traffic or private transcripts to external services. If unavailable, identify the dated baseline and do not claim current verification. |
| Deploy or transmit | Outside this skill. Requires a separate authorized workflow, operator approval, appropriate station/network permissions and any applicable licences. This skill grants none of these. |

Never key PTT, send messages, generate speech, invoke cloud services, inspect
credentials or live systems, or deploy/change an existing live prompt policy.
Proposed instructions are reviewable text, not an instruction to activate them.
Do not invent licence status, call signs, clearances, positions, reception quality,
dispatch actions, acknowledgments from others, or successful delivery.

## Workflow

1. **Choose the output mode and establish facts.**
   - “Make this radio-ready” → draft only the supplied message.
   - “Review this PTT exchange” → identify material errors and provide corrected text.
   - “Write instructions for a radio assistant” → propose a text-only policy using
     this workflow and its explicit local/official distinction.
   - Distinguish a short assistant answer from a full on-air draft. Do not append
     identifiers or `OVER`/`OUT` to ordinary chat answers unnecessarily.
   - For a full draft, use supplied sender/recipient identifiers and reply intent.
     If essential facts are absent, give a targeted clarification or a clearly
     marked non-transmittable placeholder template, never an invented value.

2. **Check priority before formatting.**
   - RIC-22 §3.1 orders traffic: distress, urgency, safety, then all other traffic.
   - On a report of actual distress, prioritize immediate appropriate emergency
     assistance and non-interference. Do not delay safety guidance for formatting,
     ask routine questions, or promise that help was dispatched.
   - Stations hearing distress must cease interfering transmissions and listen
     (§5.2.3). Uninvolved stations must not resume normal traffic until cancellation
     permits it (§5.2.11). Present this as operator guidance, not actions already taken.
   - Do not simulate live distress/emergency traffic, produce a ready-to-transmit
     fictitious emergency call, or claim command/control authority. Training is
     offline explanatory prose only, clearly labeled illustrative and non-operational.

3. **Build an intelligible exchange.**
   - Plan the content; listen before transmitting and wait if routine traffic
     would interfere (§§4.7, 4.14). A text assistant cannot verify channel clearance.
   - Call the recipient first, then `THIS IS`, then the sender; invite a reply
     with `OVER` (§§4.7–4.8). General message handling is call, reply, message,
     acknowledgment/ending (§4.14); do not invent the reply to skip this sequence.
   - Retain supplied identification at initial contact and conclusion; use
     positive identification on shared channels (§4.6). Spell call-sign letters
     phonetically (§4.3); do not substitute a fictitious identity for a missing one.
   - Use `GO AHEAD` to invite the message, not to authorize movement or other action.
     `STAND BY` requests waiting; include an estimated delay only when known.
   - When unsure whether a call is for this station, do not reply until repeated
     and understood. If definitely addressed to this station but the caller is
     unknown, use `STATION CALLING [own identification], SAY AGAIN, OVER` (§4.11).
   - Preserve ordinary speaking rhythm and a steady, clear pace; avoid shouting,
     rushed speech and filler (§4.1). For PTT instructions, have the operator
     follow their device/network SOP to avoid clipped speech; invent no fixed
     key-up delay, channel, or frequency. This device advice is a local adaptation.

4. **Use precise procedural words and values.**
   - `OVER`: this transmission ends and a response is expected.
     `OUT`: the conversation ends and no response is expected. Never combine them.
   - `ROGER` acknowledges receipt of the complete last transmission, not agreement
     or compliance. `WILCO` additionally commits to understanding and compliance;
     use only when that commitment is supplied and authorized.
   - Use `SAY AGAIN` for repetition, `CORRECTION` to repair an error, and `READ BACK`
     to request an exact repetition. Do not replace an exact read-back with `ROGER`.
   - Load [Source guide § Values and repair](references/RIC_22_GUIDE.md#values-and-repair)
     for phonetics, numbers, time, corrections, and read-back. Load
     [Source guide § Procedural words](references/RIC_22_GUIDE.md#procedural-words)
     when reviewing additional prowords or common radio habits.

5. **Apply local brevity and verify.**
   - Put the answer or necessary request first. Aim for at most 20 words for
     routine replies, counting spoken digit/phonetic words when writing spoken text.
   - Preserve caveats and safety exceptions. Do not imply a fact is verified merely
     because a concise sentence sounds confident. Necessary clarification is not
     a chatty follow-up.
   - For an explicit detail request, provide sufficient detail, organized plainly;
     the routine word target is not a cap on requested explanations.
   - Check identities, facts, priority, exact values, proword meaning, and ending
     against reply intent. Validate skill changes with the offline acceptance cases.

## Output format

- **Routine assistant answer:** just the answer, unless a necessary caveat applies.
- **Full radio draft:** label `Draft — not transmitted`, followed by the supplied
  identifiers and appropriate message. Keep annotations outside the spoken text.
- **Review:** concise finding plus corrected draft; ask only an essential clarification.
- **Proposed assistant instructions:** separate “RIC-22 baseline”, “Local preferences”,
  and “Safety and authorization boundaries”; never claim the proposal is deployed.
- **Illustrations:** label all fictitious examples as offline/non-operational.
  Placeholders are not assigned identifiers and must not be transmitted.

Illustrative offline example, not operational traffic:
`[recipient identifier] THIS IS [sender identifier]. GO AHEAD. OVER.`
This invites a message, not permission to take action.

## Error handling

| Condition | Response |
|---|---|
| Missing or ambiguous identifier | Clarify only what is needed; use the two distinct §4.11 cases above for received calls. |
| Unknown answer or conflicting facts | State uncertainty; do not guess or turn “unverified” into an affirmative status. |
| `OVER AND OUT`, slang, or unapproved codes | Correct the phrase according to intent; use plain wording absent an approved local SOP. |
| `ROGER` presented as agreement, clearance, or compliance | Explain receipt-only meaning; require explicit authority/facts for a stronger claim. |
| Unclear number, unit, time zone, or partial reception | Ask for the specific missing portion; preserve exact values in read-back. |
| Brevity would remove safety information | Exceed the local target; keep the warning and qualifications. |
| Distress test requested on a live channel | Do not simulate or transmit it; offer only offline explanatory text. |
| Source or SOP unavailable | Use the dated baseline, disclose limitations, and avoid claiming service-specific compliance. |
| Deployment, speech, or sending requested | State that this is drafting-only; do not invoke operational tools. |

## Post-Run Reflection

Silently check whether the correct mode triggered, facts and identities were
preserved, priority and prowords were correct, local preferences were distinguished
from ISED guidance, and safety/authorization boundaries held. Correct the deliverable
before returning it. Do not append a reflection, offer, or unsolicited follow-up to a
routine response. Record validation limitations when reviewing the skill itself;
do not auto-edit live policies or create issues. This protocol is self-contained.

## References

Load only the relevant reference when needed; neither requires another local file.

| Reference | When to load |
|---|---|
| [Source guide § Provenance](references/RIC_22_GUIDE.md#provenance) | Source scope, dates, section mapping, and conflicts with habits |
| [Source guide § Values and repair](references/RIC_22_GUIDE.md#values-and-repair) | Exact phonetics, numbers, time, correction, or read-back |
| [Offline acceptance cases § Cases](references/ACCEPTANCE_CASES.md#cases) | Authoring, reviewing, or regression-testing this skill |
