# Lorem.space server
The backend of https://lorem.space lives here.

## Features
* Scan the directory & categorize the image resources (only JPEG for now)
* Provide HTTP API
* Resize the image resources
* Crop around a focal point (useful when a subject, such as a face, is off-center)
* Cache option to store the resized image as a file

## HTTP API

`GET /image` returns a random image from a random category.
`GET /image/{category}` returns a random image from the given category.

Query parameters:

* `w`: target width (integer, between `min-width` and `max-width`)
* `h`: target height (integer, between `min-height` and `max-height`)
* `fx`: horizontal focal point, from `0` (left edge) to `1` (right edge)
* `fy`: vertical focal point, from `0` (top edge) to `1` (bottom edge)

When both `w` and `h` are present, the image is cropped to the target aspect
ratio. Without `fx`/`fy` the crop is centered, exactly as before. When a focal
point is given, the crop window is centered on it and shifted inward wherever
it would run past the source edge, so the subject stays visible. `fx` and `fy`
are independent and default to `0.5` when omitted.

The following parameter names are accepted for the same values:
`fx` / `focus-x` / `focus_x` / `focusX` / `fpx` / `focal-x` / `focalPointX`,
and `fy` / `focus-y` / `focus_y` / `focusY` / `fpy` / `focal-y` /
`focalPointY`. Both axes can also be given together as
`focus=<fx>,<fy>` (or `focal=`, `focalPoint=`).

Examples:

```
# Face is on the right side of the photo
GET /image/portraits?w=300&h=300&fx=0.8&fy=0.4
GET /image/portraits?w=300&h=300&focus=0.8,0.4

# Keep the bottom of a tall photo
GET /image?w=1200&h=630&fy=0.75
```

Behavior:

* No focal point (or `fx=0.5&fy=0.5`): center crop, stored in the original
  cache location (`.cache/{w}x{h}/{file}`).
* A non-center focal point: the crop is stored separately per focal point
  (`.cache/{w}x{h}/focus/{fx}x{fy}/{file}`). Equivalent values such as `0.8`,
  `0.80` and `.8` share one cache entry.
* A focal point requires both `w` and `h`; otherwise `400 Bad Request` is
  returned.
* A focus value outside `[0, 1]`, a non-numeric value, `NaN` or `Inf` returns
  `400 Bad Request` with a JSON error body. Conflicting values for the same
  axis (for example `fx=0.2&focus-x=0.8`) are rejected the same way.
* Extreme aspect ratios are clamped to the source image, and images smaller
  than the requested size are upscaled instead of failing.
* The response is always JPEG (`Content-Type: image/jpeg`) with a
  `Content-Length` header.

## How to run

`$ go run main.go`

`$ go run main.go --dir="/IMAGE/DIRECTORY/PATH"`

### Options

* `host`: host:port for the HTTP server (string)
* `dir`: the path of image resources directory (string)
* `cache`: enable cache (store the result as files in .cache directory) (bool)
* `cdn`: CDN address to redirect to the cached file path, leave empty to write the file in the HTTP response (string)
* `min-width`: minimum supported width (integer)
* `max-width`: maximum supported width (integer)
* `min-height`: minimum supported height (integer)
* `max-height`: maximum supported height (integer)

### Run binary
To run built binary, visit the [release page](https://github.com/manasky/lorem-server/releases) & download your OS build file. Unzip the downloaded file & make sure about the file execution permissions, then run the app with the arguments in the terminal:

* MacOS & Linux: `./lorem-server --host "127.0.0.1:8080" --dir "~/Pictures"`
* Windows: `lorem-server.exe -host "127.0.0.1:8080" -dir "C:\Users\USER\Pictures"`
### Docker
You can pull & run the public docker image from github package registry:

`$ docker pull ghcr.io/manasky/lorem-server:latest`

#### Run docker image:
To run the container you must pass the app arguments via the `.env` file. To this, create a file named `.env` & set the options in uppercase & snake case format.

An example of .env file:
```
HOST=0.0.0.0:8080
DIR=/app/images
MIN_WIDTH=20
```
By this config, the app listens to port number `8080` on the host IP address and scan `/app/images/` path (from the container) for the images and limit the `width` size to a minimum of 20 pixels.

##### Link directory
Your images directory must link to the container, to this you can use -v arguments while running the container.

#### Run command

`$ docker run --env-file=./.env -p 8080:8080 -v ~/Pictures:/app/images ghcr.io/manasky/lorem-server:latest`

* `--env-file` sets the application environment variables path. here `.env` file exists in the current path.
* `-p 8080:8080` sets up a port forward. the application container will listen to the `8080` port (set in the .env file as `HOST`). this flag maps the container's port `8080` to port `8080` on the host (here, your localhost).
* `~/Pictures:/app/images` sets up a bindmount volume that links the directory `/app/images` from inside the app container to the directory `~/Pictures` on the host machine (here, your system). The app image directory is set in the `.env` file as `/app/images`, so here the `~/Pictures` is mounted to that.


After container running, the app will scan `/app/images` which is mounted to the directory `~/Pictures` of your system & serves the HTTP server on `127.0.0.1:8080`. Visit `127.0.0.1:8080/image` for start.
