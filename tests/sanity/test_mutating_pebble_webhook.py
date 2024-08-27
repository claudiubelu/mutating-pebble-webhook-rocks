#
# Copyright 2024 Canonical, Ltd.
# See LICENSE file for licensing details
#

from k8s_test_harness.util import docker_util, env_util


def test_mutating_pebble_webhook_rock():
    """Test mutating-pebble-webhook rock."""
    rock = env_util.get_build_meta_info_for_rock_version(
        "mutating-pebble-webhook", "0.0.1", "amd64"
    )
    image = rock.image

    process = docker_util.run_in_docker(image, ["/mutating-pebble-webhook"], False)
    expected_err = (
        "Expected file to exist, but doesn't: '/etc/admission-webhook/tls/tls.crt"
    )
    assert expected_err in process.stderr
