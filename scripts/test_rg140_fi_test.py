import contextlib,importlib.util,io,os,unittest
from pathlib import Path
from unittest.mock import patch
spec=importlib.util.spec_from_file_location('canary',Path(__file__).with_name('rg140-fi-test.py'))
c=importlib.util.module_from_spec(spec);spec.loader.exec_module(c)
class ControlTests(unittest.TestCase):
    def exercise(self,failure=None):
        calls=[];cutover=False
        def sql(s):
            if 'max(version)' in s:return '000159_staged_account_transfers'
            if 'display_name' in s:return 'fi-test|'+c.SOURCE+'|active'
            if 'SELECT md5' in s:return 'unchanged'
            if 'SELECT status' in s:return 'failed' if failure=='apply' else 'succeeded'
            if "cv.client_settings->'accounts'" in s:return 't'
            raise AssertionError(s)
        def request(path,payload=None,token=None):
            nonlocal cutover
            calls.append((path,payload))
            if path=='/auth/login':return {'token':'PRIVATE_TOKEN'}
            if path=='/auth/logout':return {}
            if path=='/system/version':return {'manager':{'gitCommit':'d2a0990e91703a4b6e8744cab172b0d092953110'}}
            if path.endswith('/client-connection'):return {'endpoint':'host:8443' if cutover else 'host:443','vlessLink':'PRIVATE_TARGET' if cutover else 'PRIVATE_SOURCE'}
            if path.endswith('/transfer') and payload is None:return {'transfer':None}
            tr={'id':'operation','sourceServerId':c.SOURCE,'targetServerId':c.TARGET,'state':'target_applying','sourceVersionId':'source-version','targetVersionId':'target-version','targetJobId':'job','devices':[]}
            if payload and payload.get('action')=='verify':
                if failure=='verify':raise c.Stop('verification_failed')
                tr['state']='target_ready'
            if payload and payload.get('action')=='cutover':cutover=True;tr['state']='client_refresh_pending'
            return tr
        output=io.StringIO()
        with patch.object(c,'sql',sql),patch.object(c,'request',request),patch.dict(os.environ,{'ROUTEGATE_BOOTSTRAP_ADMIN_USERNAME':'admin','ROUTEGATE_BOOTSTRAP_ADMIN_PASSWORD':'PRIVATE_PASSWORD'}),contextlib.redirect_stdout(output):
            if failure:
                with self.assertRaises(c.Stop):c.run()
            else:c.run()
        self.assertFalse(any('PRIVATE_' in x for x in output.getvalue().splitlines()))
        self.assertFalse(any(p and (p.get('confirmed') is True or p.get('action')=='cleanup') for _,p in calls))
        self.assertEqual(calls[-1][0],'/auth/logout')
        return calls
    def test_apply_failure_retains_source(self):
        calls=self.exercise('apply');self.assertFalse(any(p and p.get('action') in ('verify','cutover') for _,p in calls))
    def test_failed_verification_prevents_cutover(self):
        calls=self.exercise('verify');self.assertFalse(any(p and p.get('action')=='cutover' for _,p in calls))
    def test_success_stops_at_client_verification(self):
        calls=self.exercise();self.assertEqual(sum(bool(p and p.get('action')=='cutover') for _,p in calls),1)
if __name__=='__main__':unittest.main()
