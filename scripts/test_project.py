"""CI partitions must cover every current and future Go package exactly once."""
from collections import Counter
import subprocess
import unittest
from unittest.mock import patch

import project


class GoTestLanes(unittest.TestCase):
    def test_partition_covers_native_packages_and_new_packages_once(self):
        packages = subprocess.check_output(["go", "list", "./..."], text=True).splitlines()
        packages.append("github.com/jinyongp/devtools/internal/future_package")
        partition = {lane: project.go_test_lane(packages, lane) for lane in project.GO_TEST_LANES}
        self.assertEqual(Counter(package for group in partition.values() for package in group), Counter(packages))
        for lane in ("cli", "values", "vault"):
            self.assertEqual(partition[lane], ["github.com/jinyongp/devtools/internal/" + lane])
        self.assertIn(packages[-1], partition["rest"])

    def test_invalid_and_empty_lanes_fail_closed(self):
        for packages, lane in ((["anything"], "typo"), (["anything"], "cli"), ([], "rest")):
            with self.subTest(lane=lane), self.assertRaises(ValueError):
                project.go_test_lane(packages, lane)

    def test_unix_lane_retains_race_and_propagates_failure(self):
        package = "github.com/jinyongp/devtools/internal/cli"
        failure = subprocess.CalledProcessError(17, "go")
        with patch.object(project.os, "name", "posix"), \
             patch.object(project.subprocess, "check_output", return_value=package + "\n"), \
             patch.object(project, "run", side_effect=failure) as run:
            with self.assertRaises(subprocess.CalledProcessError):
                project.main(["test-go-lane", "cli"])
            run.assert_called_once_with(["go", "test", "-race", package])

    def test_bad_arguments_never_start_go(self):
        with patch.object(project.subprocess, "check_output") as output:
            for args in ([], ["unknown"], ["rest", "-run=Nothing"]):
                with self.subTest(args=args), self.assertRaises(ValueError):
                    project.main(["test-go-lane", *args])
            output.assert_not_called()


if __name__ == "__main__":
    unittest.main()
