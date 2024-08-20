#
# Copyright 2024 Canonical, Ltd.
# See LICENSE file for licensing details
#

import logging

from k8s_test_harness import harness
from k8s_test_harness.util import env_util

LOG = logging.getLogger(__name__)


def test_integration_mutating_pebble_webhook(function_instance: harness.Instance):
    rock = env_util.get_build_meta_info_for_rock_version(
        "mutating-pebble-webhook", "0.0.1", "amd64"
    )

    LOG.info(f"Using rock: {rock.image}")
    LOG.warn("Integration tests are not yet implemented yet")
