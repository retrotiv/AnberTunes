# Cross-compiles Boombrick for the TrimUI Brick (tg5040) with NextUI's toolchain.
# Digest of ghcr.io/loveretro/tg5040-toolchain:latest.
FROM ghcr.io/loveretro/tg5040-toolchain@sha256:f131c6af64029a8723d0ce8d3c2682642f5f091b04714f6beedda9bec18477ab

ARG GO_VERSION
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates wget pkg-config bzip2 \
 && rm -rf /var/lib/apt/lists/* \
 && wget -qO- "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz

# go-sdl2 finds SDL2 through pkg-config. Write a .pc file if the TrimUI SDK
# doesn't ship one.
RUN pc="${SYSROOT}/usr/lib/pkgconfig/sdl2.pc"; \
    if [ ! -f "$pc" ]; then \
      mkdir -p "$(dirname "$pc")"; \
      printf 'prefix=/usr\nlibdir=${prefix}/lib\nincludedir=${prefix}/include\n\nName: sdl2\nDescription: Simple DirectMedia Layer\nVersion: 2.0.0\nLibs: -L${libdir} -lSDL2\nCflags: -I${includedir}/SDL2 -D_REENTRANT\n' > "$pc"; \
    fi

# The Spotify helper's go-librespot links libmpg123, the one library it needs
# that the sysroot lacks. Built from source, checked against this sum.
ARG MPG123_VERSION=1.33.7
ARG MPG123_SHA256=31d0e35a4ca567ec9b5ebda6c3062bb4435d6d3eacd6ef0d95cadd7854dc03ee
RUN cd /tmp \
 && wget -q "https://www.mpg123.de/download/mpg123-${MPG123_VERSION}.tar.bz2" \
 && echo "${MPG123_SHA256}  mpg123-${MPG123_VERSION}.tar.bz2" | sha256sum -c --quiet \
 && tar -xjf "mpg123-${MPG123_VERSION}.tar.bz2" \
 && cd "mpg123-${MPG123_VERSION}" \
 && ./configure --host=aarch64-linux-gnu --prefix=/usr --disable-components --enable-libmpg123 --with-cpu=aarch64 \
      CC="${CROSS_COMPILE}gcc" CFLAGS="--sysroot=${SYSROOT} -O2" LDFLAGS="--sysroot=${SYSROOT}" \
 && make -j"$(nproc)" \
 && make install DESTDIR="${SYSROOT}" \
 && cd /tmp && rm -rf mpg123-*

ENV PATH=/usr/local/go/bin:${PATH} \
    CGO_ENABLED=1 GOOS=linux GOARCH=arm64 \
    CC=${CROSS_COMPILE}gcc CXX=${CROSS_COMPILE}g++ \
    CGO_CFLAGS="--sysroot=${SYSROOT} -I${SYSROOT}/usr/include/opus -O2" CGO_LDFLAGS="--sysroot=${SYSROOT}" \
    PKG_CONFIG_SYSROOT_DIR=${SYSROOT} PKG_CONFIG_LIBDIR=${SYSROOT}/usr/lib/pkgconfig

WORKDIR /src
