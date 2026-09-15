import unittest

from deploy.deploy import AGENT_BINARY_TARGETS


class AgentBinaryTargetsTest(unittest.TestCase):
    def test_covers_supported_desktop_and_server_platforms(self) -> None:
        self.assertEqual(
            set(AGENT_BINARY_TARGETS),
            {
                ("linux", "amd64"),
                ("linux", "arm64"),
                ("darwin", "amd64"),
                ("darwin", "arm64"),
                ("windows", "amd64"),
                ("windows", "arm64"),
            },
        )
