"""execute the trusted resolver with adversarial API fixtures."""
import copy
import json
import pathlib
import subprocess
import unittest

import yaml

ROOT = pathlib.Path(__file__).resolve().parents[3]
WORKFLOW = yaml.safe_load((ROOT / '.github/workflows/preview.yml').read_text())
BUILD = yaml.safe_load((ROOT / '.github/workflows/preview-build.yml').read_text())
SCRIPT = WORKFLOW['jobs']['resolve']['steps'][0]['with']['script']
SHA = 'a' * 40
REPO = 'kallisti-chat/kallisti'


def fixture(*, command=None, fork=False):
    head_repo = 'fork/kallisti' if fork else REPO
    return dict(
        event='issue_comment' if command is not None else 'workflow_run', actor='cortexobscura',
        pr=dict(number=31, state='open', draft=True, base=dict(ref='main', repo=dict(full_name=REPO)),
                head=dict(sha=SHA, ref='feature', repo=dict(full_name=head_repo))),
        workflow=dict(id=41, path='.github/workflows/preview-build.yml', state='active'),
        run=dict(id=42, run_attempt=2, workflow_id=41, path='.github/workflows/preview-build.yml',
                 event='pull_request', status='completed', conclusion='success', head_sha=SHA,
                 repository=dict(full_name=REPO), head_repository=dict(full_name=head_repo),
                 head_branch='feature', pull_requests=[]),
        comment=dict(id=7, body=command or '/preview '+SHA, user=dict(login='cortexobscura'),
                     issue_url=f'https://api.github.com/repos/{REPO}/issues/31',
                     created_at='2026-01-02T00:00:00Z', updated_at='2026-01-02T00:00:00Z'),
        comments=[], artifacts=[dict(id=43, name='preview-image-2', expired=False, size_in_bytes=1024, workflow_run=dict(id=42, head_sha=SHA))],
        is_pr=True)


def resolve(data):
    js = '''
const input = JSON.parse(process.argv[1]);
const outputs = {}, failures = [];
const core = {setOutput: (k,v) => outputs[k]=v, setFailed: v => failures.push(v)};
const context = {eventName: input.event, actor: input.actor,
  repo: {owner: 'kallisti-chat', repo: 'kallisti'}, payload: {
  issue: {number: 31, pull_request: input.is_pr ? {} : undefined},
  workflow_run: {id: 42}, comment: {id: 7}}};
const github = {rest: {
  actions: {getWorkflow: async () => ({data: input.workflow}),
            getWorkflowRun: async () => ({data: input.run}),
            getArtifact: async () => ({data: input.artifacts[0]}),
            listWorkflowRuns: 'runs', listWorkflowRunArtifacts: 'artifacts'},
  pulls: {get: async () => ({data: input.pr}), list: 'pulls'},
  issues: {getComment: async () => ({data: input.comment}), listComments: 'comments'}},
  paginate: async (endpoint) => ({runs: [input.run], pulls: [input.pr],
                                artifacts: input.artifacts, comments: input.comments})[endpoint]};
await (async () => { SCRIPT })();
console.log(JSON.stringify({outputs, failures}));
'''.replace('SCRIPT', SCRIPT)
    run = subprocess.run(['node', '--input-type=module', '-e', js, json.dumps(data)],
                         text=True, capture_output=True, check=True)
    return json.loads(run.stdout)


class WorkflowTests(unittest.TestCase):
    def assert_denied(self, data):
        self.assertNotIn('allowed', resolve(data)['outputs'])

    def test_same_repo_draft_auto_and_fork_denied(self):
        outputs = resolve(fixture())['outputs']
        self.assertEqual(outputs['sha'], SHA)
        self.assertEqual(outputs['build_run_id'], '42')
        self.assertEqual(outputs['build_attempt'], '2')
        self.assertEqual(outputs['build_workflow_id'], '41')
        self.assertEqual(outputs['build_artifact_id'], '43')
        self.assert_denied(fixture(fork=True))

    def test_fork_approval_before_build_finishes(self):
        data = fixture(fork=True)
        data['comments'] = [data['comment'], dict(data['comment'], id=8)]
        self.assertEqual(resolve(data)['outputs']['approval'], '8')
        data['comments'][0]['updated_at'] = 'edited'
        data['comments'][1]['body'] = '/preview ' + 'b' * 40
        self.assert_denied(data)

    def test_explicit_comment_approval_and_ambiguous_commands(self):
        data = fixture(command='/preview '+SHA, fork=True)
        self.assertEqual(resolve(data)['outputs']['approval'], '7')
        for body in ['/preview', '/preview '+ 'b'*40, '/preview '+SHA+' extra']:
            data['comment']['body'] = body
            result = resolve(data)
            self.assertNotIn('allowed', result['outputs'])
            self.assertIn('/preview '+SHA, result['failures'][0])

    def test_comment_identity_edit_and_issue_binding(self):
        for key, value in [('user', dict(login='attacker')), ('updated_at', 'edited'),
                           ('issue_url', f'https://api.github.com/repos/{REPO}/issues/32')]:
            data = fixture(command='/preview '+SHA)
            data['comment'][key] = value
            self.assert_denied(data)
        for key, value in [('actor', 'attacker'), ('is_pr', False)]:
            data = fixture(command='/preview '+SHA)
            data[key] = value
            self.assert_denied(data)

    def test_wrong_build_workflow_identity(self):
        for key, value in [('id', -1), ('path', '.github/workflows/attacker.yml'), ('state', 'disabled_manually')]:
            data = fixture()
            data['workflow'][key] = value
            self.assert_denied(data)

    def test_wrong_run_identity_rejected_on_both_triggers(self):
        for command in [None, '/preview '+SHA]:
            for key, value in [('id', 0), ('run_attempt', 0), ('workflow_id', 99),
                               ('path', '.github/workflows/test.yml'), ('event', 'push'),
                               ('status', 'in_progress'), ('conclusion', 'failure'),
                               ('head_sha', 'b'*40), ('head_branch', 'wrong'),
                               ('repository', dict(full_name='other/repo')),
                               ('head_repository', dict(full_name='wrong/fork')),
                               ('pull_requests', [dict(number=32, head=dict(sha=SHA))]),
                               ('pull_requests', [dict(number=31, head=dict(sha='b'*40))])]:
                with self.subTest(command=command, field=key):
                    data = fixture(command=command)
                    data['run'][key] = value
                    self.assert_denied(data)

    def test_associated_run_accepted(self):
        data = fixture()
        data['run']['pull_requests'] = [dict(number=31, head=dict(sha=SHA))]
        self.assertEqual(resolve(data)['outputs']['sha'], SHA)

    def test_closed_wrong_base_or_base_repo(self):
        for key, value in [('state', 'closed'), ('base', dict(ref='other', repo=dict(full_name=REPO))),
                           ('base', dict(ref='main', repo=dict(full_name='other/repo')))]:
            data = fixture()
            data['pr'][key] = value
            self.assert_denied(data)

    def test_missing_expired_wrong_or_duplicate_artifact(self):
        data = fixture()
        for key, value in [('name', 'preview-image-1'), ('id', 0), ('expired', True), ('size_in_bytes', 0),
                           ('workflow_run', dict(id=99, head_sha=SHA)),
                           ('workflow_run', dict(id=42, head_sha='b'*40))]:
            changed = copy.deepcopy(data)
            changed['artifacts'][0][key] = value
            self.assert_denied(changed)
        data['artifacts'] *= 2
        self.assert_denied(data)
        data['artifacts'] = []
        self.assert_denied(data)

    def test_comment_job_gate(self):
        gate = WORKFLOW['jobs']['resolve']['if']
        self.assertIn('github.event.issue.pull_request', gate)
        self.assertIn("startsWith(github.event.comment.body, '/preview')", gate)
        self.assertIn('github.actor', gate)
        self.assertIn('cortexobscura', gate)
        self.assertIn('lambadalambda', gate)
        self.assertFalse(WORKFLOW['concurrency']['cancel-in-progress'])

    def test_untrusted_work_only_in_plain_pull_request(self):
        self.assertEqual(set(BUILD[True]), {'pull_request'})
        self.assertEqual(BUILD['permissions'], {'contents': 'read'})
        for job in BUILD['jobs'].values():
            self.assertEqual(job['permissions'], {'contents': 'read'})
            checkout = job['steps'][0]
            self.assertEqual(checkout['with']['ref'], '${{ github.event.pull_request.head.sha }}')
            self.assertFalse(checkout['with']['persist-credentials'])
        raw = (ROOT / '.github/workflows/preview-build.yml').read_text()
        self.assertNotIn('secrets.', raw)
        self.assertNotIn('packages: write', raw)
        self.assertEqual({v['task'] for v in BUILD['jobs']['check']['strategy']['matrix']['include']},
                         {'unit', 'desktop', 'chart', 'chrome', 'firefox', 'safari'})

    def test_privileged_workflow_never_checks_out_candidate(self):
        self.assertEqual(set(WORKFLOW[True]), {'workflow_run', 'issue_comment'})
        for job in WORKFLOW['jobs'].values():
            self.assertFalse(any('actions/checkout' in step.get('uses', '') for step in job['steps']))
            self.assertFalse(any('docker run' in step.get('run', '') for step in job['steps']))
        publisher = WORKFLOW['jobs']['publish']
        self.assertEqual(publisher['needs'], 'resolve')
        download = publisher['steps'][0]['with']
        self.assertEqual(download['artifact-ids'], '${{ needs.resolve.outputs.build_artifact_id }}')
        self.assertEqual(download['run-id'], '${{ needs.resolve.outputs.build_run_id }}')


if __name__ == '__main__':
    unittest.main()
