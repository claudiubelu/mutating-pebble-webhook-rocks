#
# Copyright 2024 Canonical, Ltd.
# See LICENSE file for licensing details
#

import base64
import logging
import os
import pathlib
import subprocess

import pytest
import yaml
from k8s_test_harness import harness
from k8s_test_harness.util import constants, env_util, k8s_util

LOG = logging.getLogger(__name__)

DIR = pathlib.Path(__file__).absolute().parent
TEMPLATES_DIR = DIR / ".." / "templates"
BASE_DIR = DIR / ".." / ".."
MANIFESTS_DIR = BASE_DIR / "manifests"

PEBBLE_ENV = "PEBBLE"
PEBBLE_ENV_COPY_ONCE = "PEBBLE_COPY_ONCE"

PEBBLE_VOLUME_NAME = "pebble-dir"
PEBBLE_DEFAULT_DIR = "/var/lib/pebble/default"
PEBBLE_WRITABLE_SUBPATH = "writable"
PEBBLE_DEFAULT_WRITABLE_DIR = os.path.join(PEBBLE_DEFAULT_DIR, PEBBLE_WRITABLE_SUBPATH)


@pytest.fixture(scope="module")
def webhook_instance(module_instance: harness.Instance):
    rock = env_util.get_build_meta_info_for_rock_version(
        "mutating-pebble-webhook", "0.0.1", "amd64"
    )

    # Generate certificates needed by webhook.
    for target in ["generate-selfsigned-cert", "set-webhook-cabundle"]:
        make_command = [
            "make",
            "-C",
            BASE_DIR,
            target,
        ]
        subprocess.run(make_command, check=True)

    # Some specs need to be updated before applying.
    server_cert = pathlib.Path(BASE_DIR / "tls" / "server.crt").read_bytes()
    server_key = pathlib.Path(BASE_DIR / "tls" / "server.key").read_bytes()
    file_updates = {
        "webhook-deployment.yaml": {
            # what_to_update: new_value.
            "ghcr.io/canonical/mutating-pebble-webhook": f"{rock.image} #",
        },
        "webhook-secret.yaml": {
            "tls.crt": f"tls.crt: {base64.b64encode(server_cert).decode()} #",
            "tls.key": f"tls.key: {base64.b64encode(server_key).decode()} #",
        },
    }

    # Apply updates if needed, and deploy webhook.
    for filename in [
        "webhook-ns.yaml",
        "webhook-secret.yaml",
        "webhook-deployment.yaml",
        "webhook-svc.yaml",
        "webhook.yaml",
    ]:
        spec = pathlib.Path(MANIFESTS_DIR / filename).read_text()
        if filename in file_updates:
            for old, new in file_updates[filename].items():
                spec = spec.replace(old, new, 1)

        module_instance.exec(
            ["k8s", "kubectl", "apply", "-f", "-"],
            input=spec.encode(),
        )

    k8s_util.wait_for_deployment(
        module_instance, "mutating-pebble-webhook", "pebble-webhook"
    )

    yield module_instance


def _apply_pod(instance: harness.Instance, spec_filename: str):
    """Applies the given Pod spec and waits for it to become Ready/

    Returns the new Pod spec.
    """
    # NOTE(claudiub): pod names should be unique, as we're not cleaning them up anyways.

    spec = pathlib.Path(TEMPLATES_DIR / spec_filename).read_text()
    spec_yaml = yaml.safe_load(spec)
    pod_name = spec_yaml["metadata"]["name"]

    instance.exec(
        ["k8s", "kubectl", "apply", "-f", "-"],
        input=spec.encode(),
    )

    k8s_util.wait_for_resource(
        instance,
        "pod",
        pod_name,
        condition=constants.K8S_CONDITION_READY,
        retry_delay_s=10,
    )

    # Get the Pod yaml spec and return it.
    process = instance.exec(
        ["k8s", "kubectl", "get", "-o", "yaml", "pod", pod_name],
        capture_output=True,
    )

    return yaml.safe_load(process.stdout)


def test_webhook_noop(webhook_instance: harness.Instance):
    pod = _apply_pod(webhook_instance, "pod-noop.yaml")

    # The pod should not have any environment variables, and no "pebble-dir" volume.
    assert "env" not in pod["spec"]["containers"][0]

    volume_names = [vol["name"] for vol in pod["spec"]["volumes"]]
    assert PEBBLE_VOLUME_NAME not in volume_names


def test_webhook_already_has_mount(webhook_instance: harness.Instance):
    pod = _apply_pod(webhook_instance, "pod-has-mount.yaml")
    container = pod["spec"]["containers"][0]

    # The pod already has a mount in the PEBBLE default path, and the webhook should have
    # skipped processing it. It should not have any environment variables, and no
    # "pebble-dir" volume.
    assert "env" not in container

    # Sanity check, make sure we do have a mount.
    volume_mounts = [mount["mountPath"] for mount in container["volumeMounts"]]
    assert PEBBLE_DEFAULT_DIR in volume_mounts

    volume_names = [vol["name"] for vol in pod["spec"]["volumes"]]
    assert PEBBLE_VOLUME_NAME not in volume_names


def test_webhook_mixed_containers(webhook_instance: harness.Instance):
    pod = _apply_pod(webhook_instance, "pod-mixed.yaml")

    # The normal container should not have any env vars set, and no volume mounts for Pebble.
    container = pod["spec"]["containers"][0]
    assert "env" not in container

    volume_mounts = [mount["name"] for mount in container["volumeMounts"]]
    assert PEBBLE_VOLUME_NAME not in volume_mounts

    # The read-only container should have the PEBBLE and PEBBLE_COPY_ONCE env vars set.
    container = pod["spec"]["containers"][1]
    env = [e["value"] for e in container["env"] if e["name"] == PEBBLE_ENV]
    assert len(env) == 1 and env[0] == PEBBLE_DEFAULT_WRITABLE_DIR

    env = [e["value"] for e in container["env"] if e["name"] == PEBBLE_ENV_COPY_ONCE]
    assert len(env) == 1 and env[0] == PEBBLE_DEFAULT_DIR

    # The read-only container should have the volume mount.
    volume_mounts = [mount["name"] for mount in container["volumeMounts"]]
    assert PEBBLE_VOLUME_NAME in volume_mounts

    # Redundant check, the Pod should have the volume.
    volume_names = [vol["name"] for vol in pod["spec"]["volumes"]]
    assert PEBBLE_VOLUME_NAME in volume_names


def test_webhook_update_envs(webhook_instance: harness.Instance):
    pod = _apply_pod(webhook_instance, "pod-update-envs.yaml")
    container = pod["spec"]["containers"][0]
    overwritten_pebble_path = "/var/lib/foo/lish"

    # The container should have updated PEBBLE and PEBBLE_COPY_ONCE env vars.
    env = [e["value"] for e in container["env"] if e["name"] == PEBBLE_ENV]
    assert len(env) == 1 and env[0] == os.path.join(
        overwritten_pebble_path, PEBBLE_WRITABLE_SUBPATH
    )

    env = [e["value"] for e in container["env"] if e["name"] == PEBBLE_ENV_COPY_ONCE]
    assert len(env) == 1 and env[0] == overwritten_pebble_path

    # The read-only container should have the volume mount.
    volume_mounts = [mount["name"] for mount in container["volumeMounts"]]
    assert PEBBLE_VOLUME_NAME in volume_mounts

    # Redundant check, the Pod should have the volume.
    volume_names = [vol["name"] for vol in pod["spec"]["volumes"]]
    assert PEBBLE_VOLUME_NAME in volume_names


def test_webhook_rock(webhook_instance: harness.Instance):
    pod = _apply_pod(webhook_instance, "pod-rock.yaml")

    # The container has "readOnlyRootFilesystem=true". Without the webhook, Pebble wouldn't
    # be able to start because it cannot create the files it needs.
    # Get the Pod logs. Pebble should now be able to start properly and start the service.
    process = webhook_instance.exec(
        ["k8s", "kubectl", "logs", pod["metadata"]["name"]],
        capture_output=True,
        text=True,
    )

    assert 'Service "mutating-pebble-webhook" starting:' in process.stdout
