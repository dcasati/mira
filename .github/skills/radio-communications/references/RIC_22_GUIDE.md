# RIC-22 source guide

## Provenance

Primary source: [ISED, RIC-22 — General Radio Operating Procedures](https://ised-isde.canada.ca/site/spectrum-management-telecommunications/en/licences-and-certificates/radiocom-information-circulars-ricpagination-orphans/ric-22/ric-22-general-radio-operating-procedures).

- Document edition shown in the official HTML: **Issue 4, January 2008**.
- HTML metadata `dcterms.modified`: **2017-03-23**. This is not a new edition date.
- Accessed and checked: **2026-09-15**. Both rendered text and official HTML were
  consulted because simplified extraction omitted some introductory text and examples.
- The preface describes guidance for Canadian radiocommunications and states that
  circulars have no status in law and may change without notice.
- §§1–2: general relevance to all radio operators; originally prepared for ROC-L
  candidates, a certificate that the document says is no longer issued.
- §5.1: distress/urgency/safety procedures primarily serve aeronautical and maritime
  services; land-mobile use is rare. This is not a police dispatcher SOP.
- Appendix B describes station licensing and exemptions. This skill makes no
  current licensing determination and grants no licence or authority.
- Historical agency names, penalty amounts, contacts, and licensing details are
  not reproduced as current legal advice. Verify current applicable requirements
  separately before any authorized operational activity.

The rules below are selective paraphrases, not a reproduction of the circular.
The word target, conversational style, PTT-device advice, and offline-only limits
are local skill choices, not claims about ISED requirements or Zello regulation.

## Section map

| Source section | Verified guidance used by this skill |
|---|---|
| §§3.1, 3.4–3.5 | Priority order; avoid unnecessary/interfering traffic; prohibition on false distress signals |
| §3.3 | Base station normally controls base/mobile communications; distress/urgency are exceptions |
| §4.1 | Clear articulation, steady pace, ordinary rhythm, no shouting/rushing/filler |
| §§4.2–4.4 | 24-hour time, time-zone handling, ITU spelling, digits and whole thousands |
| §4.5; Appendix A | Standard procedural words instead of the listed slang |
| §§4.6–4.8 | Identification, listen first, recipient before sender, single-station calling |
| §§4.9–4.10 | Multiple calls; normally reply in called order; all-stations no-reply traffic ends `OUT` |
| §4.11 | Reply/invite or wait; different treatment for uncertain addressee versus unknown caller |
| §§4.13–4.14 | Corrections, specific repetition requests, planning and four-part exchange |
| §4.15 | Signal-check procedure and readability scale; checks should not exceed 10 seconds |
| §§5.2.3, 5.2.11 | Distress takes absolute priority; cease interference/listen; uninvolved stations await cancellation |
| §§6.1–6.2, 7.1–7.2 | Urgency below distress; safety below urgency; avoid interfering with these messages |

## Procedural words

Meanings from Appendix A, paraphrased:

| Word or phrase | Use and boundary |
|---|---|
| ACKNOWLEDGE | Request confirmation of receipt and understanding. |
| AFFIRMATIVE | Yes, or permission granted in the proper context. Never fabricate that permission. |
| NEGATIVE | No, incorrect, or disagreement. |
| GO AHEAD | Invite the other station's message, not physical movement or task clearance. |
| STAND BY | Ask the other station to wait while you pause and subsequently call. |
| OVER | End this transmission and invite/expect a response. |
| OUT | End the conversation without expecting a response. |
| ROGER | Confirm receipt of the whole last transmission; not a synonym for yes. |
| WILCO | Confirm receipt, understanding, and intent to comply. Do not promise actions outside actual authority. |
| SAY AGAIN / I SAY AGAIN | Request repetition / signal your own repetition, rather than “repeat”. |
| READ BACK | Request exact repetition of all or a specified portion, not a paraphrase. |
| CONFIRM | Check whether information was correctly received or a message was received. |
| CORRECTION | Mark an error and provide the corrected version. |
| VERIFY | Check the coding/text with the originator and send the correct version. |
| THAT IS CORRECT | Confirm correctness. Do not use without a basis for comparison. |
| DISREGARD | Treat the specified transmission as not sent. |
| WORDS TWICE | Ask for, or announce, repetition of each word/group under difficult conditions. |
| BREAK | Separate message portions where the boundary is unclear; not a generic emergency interruption signal. |
| CLEARED | Authorization under stated conditions, not an acknowledgment; never invent it. |

§4.5 explicitly discourages “OK”, “REPEAT”, “TEN-FOUR”, “OVER AND OUT”,
“BREAKER BREAKER”, and “COME IN PLEASE”. Use “SAY AGAIN” for a repetition request.
`OVER` and `OUT` contradict one another. Familiarity in entertainment or a local
radio habit does not establish conformance to RIC-22.

An approved local SOP may prescribe different vocabulary. Identify that difference
in a review; do not label local codes as RIC-22 or infer an SOP from a user's slang.
RIC-22 does not prescribe a 20-word response limit, a universal PTT delay, or
mandatory casual “10-4” acknowledgments.

## Values and repair

### Phonetic spelling

§4.3 uses the ITU alphabet for difficult/unusual words, isolated letters, and call
signs. The standardized spellings are **Alfa**, **Juliett**, and **X-ray**.
These mapping facts are included for exact spelling, not as invented identifiers.

| Letters | Words |
|---|---|
| A B C D E F | Alfa, Bravo, Charlie, Delta, Echo, Foxtrot |
| G H I J K L | Golf, Hotel, India, Juliett, Kilo, Lima |
| M N O P Q R | Mike, November, Oscar, Papa, Quebec, Romeo |
| S T U V W X | Sierra, Tango, Uniform, Victor, Whiskey, X-ray |
| Y Z | Yankee, Zulu |

RIC-22's digit pronunciation cues: 0 ZE-RO; 1 WUN; 2 TOO; 3 TREE; 4 FOW-er;
5 FIFE; 6 SIX; 7 SEV-en; 8 AIT; 9 NIN-er. Its cues for decimal, hundred,
and thousand are DAY-SEE-MAL, HUN-dred, and TOU-SAND.
Use ordinary written digit names in a draft; provide pronunciation cues if useful
or requested. Do not rewrite unrelated ordinary prose phonetically.

### Numbers and time

- §4.4: pronounce digits separately, except whole thousands: speak the digits
  forming the number of thousands and then “thousand”.
- Say “decimal” for a decimal point; preserve units and monetary sequence.
- §§4.2–4.4: use four-figure 24-hour time with individually spoken digits.
  Use UTC followed by “zulu” unless operations are solely in one time zone,
  where local/standard time may be used. Do not infer a missing zone.
- §4.2's date-time group comprises two digits for the day of the month followed
  by four for the time. Do not infer a month, year, or time-zone conversion.
- Offline illustrative conversions: 42 → “four two”; 300 → “three zero zero”;
  12,000 → “one two thousand”; 42.6 → “four two decimal six”;
  14:05 UTC → “one four zero five zulu”.
- A listed pronunciation for “hundred” is not permission to ignore §4.4's
  digit-by-digit rule for a value such as 300. Time and identifiers are not
  quantities to convert using the whole-thousands exception.

### Corrections and exact read-back

- §4.13: mark a transmission error with `CORRECTION`, repeat the last correct
  word/phrase, and give the corrected version. Do not silently change a sent value.
- For a complete repetition request, use `SAY AGAIN`. For a missing portion, use
  a specific item name, `ALL BEFORE`, `ALL AFTER`, or the bounding words.
- Appendix A's `READ BACK` requests exact received content, all or specified part.
  Preserve values and their sequence; if something was not received clearly,
  request it rather than completing it by inference.
- **Local safeguard:** for safety-relevant ambiguity, prefer a targeted
  clarification/read-back over a guess. RIC-22 defines read-back but does not
  establish a universal requirement to read back every message.

## Emergency scope and boundaries

§5.1 defines distress as grave and/or imminent danger needing immediate assistance;
urgency concerns safety without needing immediate assistance; formal safety traffic
concerns navigation safety or important meteorological warnings.
The source identifies `MAYDAY`, `PAN PAN`, and `SECURITE` for these categories.
Mentioning their meanings in offline prose is not issuing a signal.

Do not generate fictitious distress calls, cancellation announcements, relays, or
silence commands for live use. Do not claim that a chat assistant acknowledged,
controlled, transmitted, or resolved an actual incident. The prohibition on false
distress is sourced (§3.5); this skill's ban on live emergency simulation is a
separate, deliberately conservative local boundary.

For reported real danger, offer concise safety-oriented guidance to contact the
appropriate emergency service/operator and avoid interference; retain relevant
facts without inventing a location, emergency number, resource dispatch, or clearance.
Do not delay necessary help to enforce an instructional format or word target.
