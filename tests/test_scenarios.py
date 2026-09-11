import unittest

from scenario_runner import discover_scenarios, run_scenario_file


class ScenarioTests(unittest.TestCase):
    def test_all_scenarios(self):
        paths = discover_scenarios()
        self.assertTrue(paths, "no scenario files found")
        for path in paths:
            with self.subTest(scenario=path.name):
                result = run_scenario_file(path)
                self.assertTrue(
                    result.passed,
                    f"{path.name} failed: {result.failure_text()}",
                )


if __name__ == "__main__":
    unittest.main()
