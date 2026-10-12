// The Gitea runners use Docker's default bridge, without service-name DNS.
// Resolve only this run's labeled service through the runner's Docker socket.
import http from 'node:http';

const run = process.env.TEST_DATABASE_RUN;
if (!/^\d+-\d+$/.test(run ?? '')) {
  throw new Error('TEST_DATABASE_RUN must contain the run ID and attempt');
}

function docker(path) {
  return new Promise((resolve, reject) => {
    const request = http.get({ socketPath: '/var/run/docker.sock', path }, response => {
      let body = '';
      response.setEncoding('utf8');
      response.on('data', chunk => { body += chunk; });
      response.on('error', reject);
      response.on('end', () => {
        if (response.statusCode !== 200) {
          reject(new Error(`Docker inspection failed: HTTP ${response.statusCode}`));
          return;
        }
        try { resolve(JSON.parse(body)); } catch (error) { reject(error); }
      });
    });
    request.on('error', reject);
    request.setTimeout(10000, () => request.destroy(new Error('Docker inspection timed out')));
  });
}

const label = `kork.ci-database=${run}`;
const filters = encodeURIComponent(JSON.stringify({ label: [label] }));
const containers = await docker(`/containers/json?filters=${filters}`);
if (containers.length !== 1 || !/^[a-f0-9]{64}$/.test(containers[0].Id)) {
  throw new Error('Expected exactly one running database container for this CI run');
}
const container = await docker(`/containers/${containers[0].Id}/json`);
if (container.Config.Labels['kork.ci-database'] !== run) {
  throw new Error('Database container label does not match this CI run');
}
const addresses = Object.values(container.NetworkSettings.Networks).map(network => network.IPAddress);
if (addresses.length !== 1 || !/^(\d{1,3}\.){3}\d{1,3}$/.test(addresses[0])) {
  throw new Error('Expected exactly one IPv4 address for the CI database');
}
console.log(`postgres://zelda:ci-only-password@${addresses[0]}:5432/zelda_test?sslmode=disable`);
