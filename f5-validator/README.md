# validate-irules

## What is this used for?

The purpose of this tool is to validate iRule ordering for VIPs in the terraform VIP config files against the f5.  
f5 is the source of truth when it comes to iRule ordering.

see [here](https://bb.dev.norvax.net/projects/DEP/repos/terraform-f5/browse/workspaces/huron/modules/f5/modules/vips) 
for the terraform vip config files.

## Why is this necessary?

*Ordering matters in the f5 for iRules attached to VIPS, otherwise resources may not resolve properly.  
Terraform currently does not support ordering when applying configurations to the f5.  It is important that the ordering 
of the iRules are preserved.  
*The f5 UI will have the correct iRule ordering(as they where applied during the creation of the VIP)

## How to run.

*The tools will assume that the f5 password credentials are set as an environment variable.
```$xslt
export f5_password=password
```
*The username, f5 host, and direcotory of VIPs to validate are then passed in at runtime.
```
go run main.go -username username -f5Host bigip.ops.norvax.net -terraformDir terraform-f5/workspaces/huron/modules/f5/modules/vips
```

