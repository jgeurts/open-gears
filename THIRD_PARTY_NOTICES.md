# Third-party components

Open Gears code is licensed under MIT. Release bundles dynamically link libusb
1.0.30 (LGPL-2.1-or-later), with upstream Darwin shutdown fix
`94a5224ea1a7515c38c618694e8794e058c16412`. Its license and complete source archive
plus patch accompany releases. The shared library can be replaced with a
compatible build; modifying a signed Mac bundle requires signing it again.

The Go executable includes github.com/google/gousb (Apache-2.0). Its license is
included in release bundles as `licenses/gousb-Apache-2.0.txt` or in the app's
Resources folder.

Shimano controller firmware is supplied by the user and is not distributed in
this repository or its releases. Shimano, Di2, E-TUBE, and Specialized names
belong to their respective owners. This is an independent project.
