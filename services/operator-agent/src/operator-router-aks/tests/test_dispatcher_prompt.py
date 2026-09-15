"""Offline prompt-contract checks without initializing clients or model calls."""

import ast
from pathlib import Path
import unittest


def router_instructions():
    source = Path(__file__).resolve().parents[1] / "main.py"
    for node in ast.parse(source.read_text()).body:
        if isinstance(node, ast.Assign) and any(
            isinstance(target, ast.Name) and target.id == "ROUTER_INSTRUCTIONS"
            for target in node.targets
        ):
            return ast.literal_eval(node.value)
    raise AssertionError("ROUTER_INSTRUCTIONS not found")


class DispatcherPromptTests(unittest.TestCase):
    def test_brief_answers_without_unsolicited_followups(self):
        instructions = router_instructions()
        self.assertIn("20 words or fewer", instructions)
        self.assertIn("Lead with the requested fact or status, then stop.", instructions)
        self.assertIn("Never end SPOKEN with an unsolicited offer", instructions)
        self.assertNotIn("SPOKEN must always end by offering more", instructions)
        self.assertNotIn("Recommend recovery. Want more?", instructions)

    def test_qualifications_and_explicit_followup_are_preserved(self):
        instructions = router_instructions()
        self.assertIn("SPOKEN: <one short radio-style line>", instructions)
        self.assertIn("DETAIL: <the fuller answer", instructions)
        self.assertIn("Preserve safety warnings, uncertainty, scope, and required approvals", instructions)
        self.assertIn("An acknowledgement alone", instructions)
        self.assertIn("is not a request for more", instructions)
        self.assertIn("If a tool call fails or returns an error, SPOKEN should say so plainly", instructions)


if __name__ == "__main__":
    unittest.main()
