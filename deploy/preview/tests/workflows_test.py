"""exercise the actual trusted resolver, without GitHub writes or candidate code."""
import json
import pathlib
import subprocess
import unittest

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[3]
WORKFLOW = yaml.safe_load((ROOT / '.github/workflows/preview.yml').read_text())
SCRIPT = WORKFLOW['jobs']['resolve']['steps'][0]['with']['script']
SHA = 'a' * 40


def resolve(command=None, *, updated='2026-01-01T00:00:00Z', author='cortexobscura', edited=False, fork=False):
    pr = dict(state='open', draft=True, base=dict(ref='main'),
              head=dict(sha=SHA, repo=dict(full_name='fork/kallisti' if fork else 'kallisti-chat/kallisti')),
              updated_at=updated)
    comment = dict(id=7, body=command, user=dict(login=author), created_at='2026-01-02T00:00:00Z',
                   updated_at='2026-01-03T00:00:00Z' if edited else '2026-01-02T00:00:00Z')
    event = 'pull_request_target' if command is None else 'issue_comment'
    js = '''
const input = JSON.parse(process.argv[1]);
const outputs = {}, failures = [];
const core = {setOutput: (k,v) => outputs[k]=v, setFailed: v => failures.push(v)};
const context = {eventName: input.event, repo: {}, payload: {
  pull_request: {number: 31}, issue: {number: 31, pull_request: {}},
  repository: {full_name: 'kallisti-chat/kallisti'}, comment: {id: 7}}};
const github = {rest: {pulls: {get: async () => ({data: input.pr})},
  issues: {getComment: async () => ({data: input.comment})}}};
await (async () => { SCRIPT })();
console.log(JSON.stringify({outputs, failures}));
'''.replace('SCRIPT', SCRIPT)
    run = subprocess.run(['node', '--input-type=module', '-e', js,
                          json.dumps(dict(pr=pr, comment=comment, event=event))],
                         text=True, capture_output=True, check=True)
    return json.loads(run.stdout)


class WorkflowTests(unittest.TestCase):
    def test_same_repo_draft_auto_and_fork_denied(self):
        self.assertEqual(resolve()['outputs']['sha'], SHA)
        self.assertNotIn('allowed', resolve(fork=True)['outputs'])

    def test_bare_approval_bound_to_observed_head(self):
        result = resolve('/preview', fork=True)
        self.assertEqual(result['outputs']['sha'], SHA)
        self.assertEqual(result['outputs']['approval'], '7')

    def test_delayed_comment_and_same_second_fail_closed(self):
        for updated in ['2026-01-03T00:00:00Z', '2026-01-02T00:00:00Z']:
            result = resolve('/preview', updated=updated, fork=True)
            self.assertNotIn('allowed', result['outputs'])
            self.assertIn('/preview ' + SHA, result['failures'][0])

    def test_explicit_current_sha_and_identity(self):
        self.assertEqual(resolve('/preview ' + SHA, fork=True, updated='2026-01-03T00:00:00Z')['outputs']['sha'], SHA)
        self.assertNotIn('allowed', resolve('/preview ' + 'b' * 40)['outputs'])
        self.assertNotIn('allowed', resolve('/preview ' + SHA, author='attacker')['outputs'])
        self.assertNotIn('allowed', resolve('/preview ' + SHA, edited=True)['outputs'])

    def test_comments_cannot_cancel_automatic_or_manual_builds(self):
        self.assertEqual(WORKFLOW["concurrency"]["group"],
                         "preview-${{ github.event_name }}-${{ github.event.pull_request.number || github.run_id }}")

    def test_privileged_job_never_checks_out_or_executes_candidate(self):
        jobs = WORKFLOW['jobs']
        for name in ['check', 'build']:
            self.assertEqual(jobs[name]['permissions'], {'contents': 'read'})
            checkout = jobs[name]['steps'][0]
            self.assertEqual(checkout['with']['ref'], '${{ needs.resolve.outputs.sha }}')
            self.assertFalse(checkout['with']['persist-credentials'])
        publisher = jobs['publish']
        self.assertEqual(publisher['needs'], ['resolve', 'check', 'build'])
        self.assertFalse(any('actions/checkout' in step.get('uses', '') for step in publisher['steps']))
        self.assertFalse(any('docker run' in step.get('run', '') for step in publisher['steps']))
        self.assertEqual({v['task'] for v in jobs['check']['strategy']['matrix']['include']},
                         {'unit', 'desktop', 'chart', 'chrome', 'firefox', 'safari'})


if __name__ == '__main__':
    unittest.main()
