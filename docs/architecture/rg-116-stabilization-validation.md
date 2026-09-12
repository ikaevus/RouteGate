# RG-116 stabilization validation gate

The stabilization hold is lifted only after the rollback path proves it can return the database and platform to the pre-deploy state after a forced failure, followed by a clean redeploy of the same release candidate. CI success alone is necessary but not sufficient for this gate.
