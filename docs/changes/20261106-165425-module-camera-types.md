### Added
- [sdk] A module can offer camera types with `camera_types` in its manifest. A camera of such a type runs on the core's generic camera, and the module feeds it its pictures with no grant. The module can draw its live view in the new `camera-view` slot.
- The camera listing names each camera's type, and `GET /api/v1/camera-types` lists the types a new camera may be declared as. The Cameras card offers them.
