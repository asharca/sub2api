import argparse
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import yaml

ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('release_matrix', Path(__file__).with_name('release_matrix.py'))
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseMatrixTest(unittest.TestCase):
    def setUp(self):
        self.previous = Path.cwd()
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        os.chdir(self.temp.name)
        self.addCleanup(os.chdir, self.previous)
        shutil.copyfile(ROOT / release.STANDALONE_CONFIG, release.STANDALONE_CONFIG)
        Path('backend/cmd/server').mkdir(parents=True)
        release.VERSION_FILE.write_text('1.0.0\n')

    def fixture_artifacts(self):
        directory = Path('release-input')
        directory.mkdir()
        target, = release.targets()
        name = release.archive_name('1.0.0', target)
        archive = directory / name
        with tarfile.open(archive, 'w:gz') as out:
            info = tarfile.TarInfo('sub2api')
            info.size = 7
            info.mode = 0o755
            out.addfile(info, io.BytesIO(b'fixture'))
        metadata = {'version': '1.0.0', 'sha': 'a' * 40, 'target': target,
                    'archive': name, 'sha256': release.sha256(archive)}
        (directory / 'manifest-linux-amd64.json').write_text(json.dumps(metadata))
        return argparse.Namespace(input='release-input', version='1.0.0', sha='a' * 40, output='contexts')

    def git(self, *args):
        return subprocess.check_output(['git', *args], text=True, stderr=subprocess.PIPE).strip()

    def fixture_git(self):
        self.git('init', '-b', release.RELEASE_BRANCH)
        self.git('config', 'user.name', 'Release Test')
        self.git('config', 'user.email', 'release-test@example.test')
        self.git('add', str(release.VERSION_FILE))
        self.git('commit', '-m', 'standalone version')
        sha = self.git('rev-parse', 'HEAD')
        self.git('update-ref', f'refs/remotes/origin/{release.RELEASE_BRANCH}', sha)
        return sha

    def test_annotated_standalone_tag_resolves_source_branch_and_artifact(self):
        sha = self.fixture_git()
        self.git('tag', '-a', 'v1.0.0', '-m', 'standalone release')
        with patch.dict(os.environ, {'GITHUB_OUTPUT': 'outputs', 'GITHUB_REPOSITORY_OWNER': 'ExampleOwner'}):
            release.plan(argparse.Namespace(ref='v1.0.0', dry_run=False))
        output = dict(line.split('=', 1) for line in Path('outputs').read_text().splitlines())
        self.assertEqual(output['sha'], sha)
        self.assertEqual(output['tag'], 'v1.0.0')
        self.assertEqual(output['version'], '1.0.0')
        self.assertEqual(output['owner_lower'], 'exampleowner')
        self.assertEqual(output['target_branch'], 'release/standalone')
        self.assertEqual(output['dry_run'], 'false')
        target, = json.loads(output['matrix'])['include']
        self.assertEqual(target, {'goos': 'linux', 'goarch': 'amd64'})
        self.assertEqual(release.archive_name(output['version'], target), 'sub2api_1.0.0_linux_amd64.tar.gz')
        self.assertEqual(self.git('status', '--porcelain', '--', str(release.VERSION_FILE)), '')

    def test_leaf_keeps_packaging_and_selects_only_one_target(self):
        original = release.config()
        release.generate_config(argparse.Namespace(mode='build', goos='linux', goarch='amd64', output='leaf.yaml'))
        leaf = yaml.safe_load(Path('leaf.yaml').read_text())
        self.assertEqual(leaf['builds'][0]['goos'], ['linux'])
        self.assertEqual(leaf['builds'][0]['goarch'], ['amd64'])
        self.assertEqual(leaf['builds'][0]['ignore'], [])
        self.assertEqual(leaf['archives'], original['archives'])
        self.assertEqual(leaf['release'], original['release'])
        self.assertFalse(leaf['dockers'])
        self.assertIn('{{ .Env.RELEASE_DATE }}', '\n'.join(leaf['builds'][0]['ldflags']))

    def test_publication_config_has_no_compilation_or_docker_work(self):
        original = release.config()
        release.generate_config(argparse.Namespace(mode='publish', output='publisher.yaml'))
        data = yaml.safe_load(Path('publisher.yaml').read_text())
        self.assertTrue(data['builds'][0]['skip'])
        self.assertFalse(data['archives'])
        self.assertFalse(data['dockers'])
        self.assertEqual(data['release']['header'], original['release']['header'])
        self.assertEqual(data['release']['footer'], original['release']['footer'])
        self.assertFalse(data['release']['prerelease'])
        self.assertNotIn('skip_upload', data['release'])
        self.assertEqual(data['release']['extra_files'], [{'glob': 'release-input/sub2api_*.tar.gz'}])
        self.assertEqual(data['checksum']['extra_files'], data['release']['extra_files'])

    def test_collect_and_verify_hash_and_source_binding(self):
        args = self.fixture_artifacts()
        target, = release.targets()
        name = release.archive_name(args.version, target)
        dist = Path('dist')
        dist.mkdir()
        archive = Path(args.input) / name
        shutil.copyfile(archive, dist / name)
        (dist / 'checksums.txt').write_text(f'{release.sha256(archive)}  {name}\n')
        release.collect(argparse.Namespace(version=args.version, sha=args.sha, output='collected', **target))
        args.input = 'collected'
        release.verify(args)
        file = Path(args.input) / name
        file.write_bytes(b'corrupted')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            release.verify(args)

    def test_missing_extra_and_wrong_commit_artifacts_are_rejected(self):
        args = self.fixture_artifacts()
        args.sha = 'b' * 40
        with self.assertRaises(ValueError):
            release.verify(args)
        args.sha = 'a' * 40
        Path('release-input/unexpected').write_text('not an asset')
        with self.assertRaises(ValueError):
            release.verify(args)
        Path('release-input/unexpected').unlink()
        next(Path('release-input').glob('*.tar.gz')).unlink()
        with self.assertRaises(FileNotFoundError):
            release.verify(args)

    def test_linux_context_preserves_binary_executable_mode(self):
        args = self.fixture_artifacts()
        Path('Dockerfile.goreleaser').write_text('FROM scratch\nCOPY sub2api /sub2api\n')
        Path('deploy').mkdir()
        Path('deploy/docker-entrypoint.sh').write_text('#!/bin/sh\nexec /app/sub2api\n')
        Path('backend/resources').mkdir()
        Path('backend/resources/data').write_text('fixture')
        release.contexts(args)
        binary = Path('contexts/amd64/sub2api')
        self.assertEqual(binary.read_bytes(), b'fixture')
        self.assertEqual(binary.stat().st_mode & 0o777, 0o755)
        self.assertFalse(Path('contexts/arm64').exists())

    def test_plan_rejects_wrong_ref_and_legacy_versions(self):
        self.fixture_git()
        for ref in ('main', 'v1.0.1'):
            with self.subTest(ref=ref), self.assertRaisesRegex(ValueError, 'matching VERSION'):
                release.plan(argparse.Namespace(ref=ref, dry_run=False))
        for version in ('0.2.23', '0.2.23-asharca.1', '1.0.0-rc.1', '1.00.0'):
            with self.subTest(version=version), self.assertRaisesRegex(ValueError, 'stable standalone version'):
                release.VERSION_FILE.write_text(version + '\n')
                release.plan(argparse.Namespace(ref='v' + version, dry_run=False))

    def test_plan_rejects_tag_not_on_fork_release_branch(self):
        self.fixture_git()
        self.git('switch', '-c', 'feat/conversation-storage')
        self.git('commit', '--allow-empty', '-m', 'upstream sync branch only')
        self.git('tag', 'v1.0.0')
        args = argparse.Namespace(ref='v1.0.0', dry_run=False)
        with self.assertRaisesRegex(ValueError, 'not on origin/release/standalone'):
            release.plan(args)
        self.git('switch', release.RELEASE_BRANCH)
        with self.assertRaisesRegex(ValueError, 'does not match the selected release tag'):
            release.plan(args)

    def test_dry_run_plan_resolves_matrix_without_a_new_tag(self):
        self.fixture_git()
        with patch.dict(os.environ, {'GITHUB_OUTPUT': 'outputs'}):
            release.plan(argparse.Namespace(ref='release/standalone', dry_run=True))
        output = dict(line.split('=', 1) for line in Path('outputs').read_text().splitlines())
        self.assertEqual(output['dry_run'], 'true')
        self.assertEqual(output['tag'], 'v1.0.0')
        self.assertEqual(json.loads(output['matrix'])['include'], [{'goos': 'linux', 'goarch': 'amd64'}])

    def test_standalone_image_tags_and_dry_run(self):
        fake_bin = Path('bin')
        fake_bin.mkdir()
        docker = fake_bin / 'docker'
        docker.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$DOCKER_LOG"\n')
        docker.chmod(0o755)
        for dry_run in (False, True):
            with self.subTest(dry_run=dry_run):
                log_path = Path(f'docker-{dry_run}.log').resolve()
                env = {**os.environ, 'PATH': str(fake_bin.resolve()) + os.pathsep + os.environ['PATH'],
                       'DOCKER_LOG': str(log_path), 'RUNNER_TEMP': self.temp.name,
                       'RELEASE_VERSION': '1.0.0', 'RELEASE_SHA': 'a' * 40,
                       'GITHUB_REPOSITORY': 'ExampleOwner/sub2api',
                       'DRY_RUN': str(dry_run).lower(), 'DOCKERHUB_USERNAME': 'fixturehub'}
                subprocess.run(['bash', str(ROOT / '.github/release-tools/release-images.sh')], env=env, check=True)
                log = log_path.read_text()
                self.assertEqual(log.count('buildx build'), 1)
                self.assertIn('linux/amd64', log)
                self.assertNotIn('linux/arm64', log)
                self.assertNotIn('fixturehub', log)
                self.assertNotIn('imagetools', log)
                tags = [part.split()[0] for part in log.split('--tag ')[1:]]
                self.assertEqual(tags, ['ghcr.io/exampleowner/sub2api:1.0.0-amd64', 'ghcr.io/exampleowner/sub2api:1.0.0'])
                self.assertEqual('--push' in log, not dry_run)
                self.assertEqual('type=oci' in log, dry_run)

    def test_workflow_targets_standalone_without_writing_source_refs(self):
        content = (ROOT / '.github/workflows/release.yml').read_text()
        workflow = yaml.safe_load(content)
        self.assertNotIn('sync-version-file', workflow['jobs'])
        self.assertNotIn('git push', content)
        self.assertIn('refs/heads/release/standalone', workflow['jobs']['prepare']['if'])
        steps = workflow['jobs']['release']['steps']
        publisher = next(step for step in steps if step.get('name') == 'Publish existing archives and release notes')
        self.assertEqual(publisher['env']['GITHUB_RELEASE_TARGET_COMMITISH'], '${{ needs.prepare.outputs.target_branch }}')
        runtime = next(step for step in steps if step.get('name') == 'Publish verified re-login runtimes')
        self.assertIn("env.DRY_RUN != 'true'", runtime['if'])

    def test_release_announcement_requires_explicit_opt_in(self):
        workflow = yaml.safe_load((ROOT / '.github/workflows/release.yml').read_text())
        trigger = workflow.get('on', workflow.get(True))
        self.assertIs(trigger['workflow_dispatch']['inputs']['notify_release']['default'], False)
        step = next(step for step in workflow['jobs']['release']['steps'] if step.get('name') == 'Send Telegram Notification')
        self.assertIn('inputs.notify_release == true', step['if'])


if __name__ == '__main__':
    unittest.main()
