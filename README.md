# telesock
![Docker Build Status](https://img.shields.io/docker/build/aleksi/telesock.svg)

Fast and simple SOCKS5 proxy server.

# Running with Docker

```sh
docker pull aleksi/telesock
docker run --restart=always --publish=1080:1080 --mount=type=bind,src=`pwd`/telesock.yaml,dst=/telesock.yaml --name=telesock aleksi/telesock
```

# License

Written in 2018 by Alexey Palazhchenko.

To the extent possible under law, the author(s) have dedicated all copyright and related and neighboring rights
to this software to the public domain worldwide. This software is distributed without any warranty.

You should have received a copy of the CC0 Public Domain Dedication along with this software.
If not, see <http://creativecommons.org/publicdomain/zero/1.0/>.
