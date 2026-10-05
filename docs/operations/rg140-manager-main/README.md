# RG-140: Manager main publication on existing schema159

The manual **RG140 Manager main update** workflow publishes exactly
`c4137cc43a2385ac38fe71e059f33405535b6618` to the US Manager. Both push workflows
37335653106 and 37335653227 must be successful for that exact candidate.
All three VPS remain in service; US remains Hybrid. No relocation is performed.

The runner only accepts existing schema159 and a schema159 bundle. It checks
there are no active transfers or reservations, drains Agent/update jobs and
rechecks after stopping Manager. Only Manager is stopped; VPN services continue.
A private consistent database dump, previous Manager binary, migrations,
frontend, unit and environment are backed up before file mutation. Failed start,
health, public-index or identity checks restore that immediate backup. This is
installation recovery, not a later restore over subsequent administrator work.

Account placement/credentials, token/device identities and transfer history
are compared privately. Agent/VPN/nginx/maintenance files, unit identities and
bootstrap artifacts are compared before/after. Old frontend assets are retained
for existing tabs. Reload Manager after success. Verify sign-in, fi-test on FI
and its existing subscriptions; do not move real users as part of publication.

The workflow builds a full verified bundle but only installs its Management
files. It does not publish bootstrap artifacts for the new commit; new-node
commissioning using that commit needs a separate bootstrap publication. Existing
nodes and subscriptions continue with their existing artifacts.

Launch from Actions → RG140 Manager main update → Run workflow → main.
Merging the helper does not deploy. A successful SSH step must report
`RESULT=updated`, exact candidate commit, schema159, public index verification,
identity preservation and unchanged unrelated services. Exit6 means Manager was
updated but an unrelated service fingerprint changed; investigate before
reporting complete. Backups stay under `/root/routegate-backups` on US.
