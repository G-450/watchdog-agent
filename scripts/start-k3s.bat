@echo off
set VMRUN="C:\Program Files (x86)\VMware\VMware Workstation\vmrun.exe"

%VMRUN% start "D:\Virtual Machines\k3s-n1\k3s-n1.vmx" nogui
%VMRUN% start "D:\Virtual Machines\k3s-n2\k3s-n2.vmx" nogui