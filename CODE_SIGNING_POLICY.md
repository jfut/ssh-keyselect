# Code signing policy

## Status

The project intends to apply to SignPath Foundation for free code signing. The application has not been approved, and code signing is not yet in use. Windows release executables are currently unsigned.

## Planned signing scope

If the application is accepted and signing is integrated, the planned scope is Windows Authenticode signing of `ssh-keyselect.exe` and `ssh-keyselect-gui.exe` for amd64 (x86-64) and arm64. Only this project's executables built from source in this repository will be submitted for signing. Linux RPM packages use jfut's RPM signing key separately. SSH authentication signatures continue to come from the user's upstream agent.

## Team responsibilities

Authors, Reviewers, and Approvers: Jun Futagawa (jfut).

External contributions, including changes to build scripts and CI configuration, are reviewed by jfut before inclusion in a release. If signing is enabled, every release signing request will require jfut's manual approval. GitHub MFA is enabled; SignPath MFA will be enabled before use.

## Attribution

If the application is accepted and signing begins, the attribution will be:

> Free code signing provided by [SignPath.io](https://about.signpath.io/), certificate by [SignPath Foundation](https://signpath.org/).

## Privacy

See the [Privacy policy](README.md#privacy-policy) for the project's data handling and network behavior.
