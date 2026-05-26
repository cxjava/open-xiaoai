FROM ubuntu:22.04 AS builder

SHELL ["/bin/bash", "-o", "pipefail", "-c"]

WORKDIR /app

COPY examples/migpt .
COPY packages/client-rust ./client-rust

# 更新源
RUN apt update && apt install curl build-essential -y

# 安装 Rust
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y

# Create a script file sourced by both interactive and non-interactive bash shells
ENV BASH_ENV=/root/.bash_env
RUN touch "$BASH_ENV"
RUN echo '. "$BASH_ENV"' >> "$HOME/.bashrc"
RUN echo '[ -s "$HOME/.cargo/env" ] && . "$HOME/.cargo/env"' >> "$BASH_ENV"

# nvm
RUN echo 'export NVM_DIR="$HOME/.nvm"' >> "$BASH_ENV"
RUN echo '[ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"' >> "$BASH_ENV"
RUN echo '[ -s "$NVM_DIR/bash_completion" ] && . "$NVM_DIR/bash_completion"' >> "$BASH_ENV"

# 安装 Node.js
RUN curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash \
    && \. $NVM_DIR/nvm.sh && \. $NVM_DIR/bash_completion \
    && nvm install 22

# 安装依赖
RUN npm install -g corepack \
    && corepack enable && corepack install \
    && pnpm install

# 构建 Rust Server
RUN sed -i 's/\.\.\/\.\.\/packages\///g' Cargo.toml \
    && pnpm build



    
FROM ubuntu:22.04 AS runner

WORKDIR /app

COPY --from=builder /app/migpt /app/migpt
COPY --from=builder /app/package.json /app/package.json
COPY --from=builder /app/tsconfig.json /app/tsconfig.json
COPY --from=builder /app/node_modules /app/node_modules

# 更新源
RUN apt update && apt install curl build-essential -y

ENV NVM_DIR=/root/.nvm

# 安装 Node.js
RUN curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash \
    && bash -c "source $NVM_DIR/nvm.sh && nvm install 22"

ENTRYPOINT ["bash", "-c", "source $NVM_DIR/nvm.sh && exec \"$@\"", "--"]

CMD ["npm", "run", "start"]
