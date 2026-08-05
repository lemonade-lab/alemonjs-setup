/**
 * @type {{ apps: import("pm2").StartOptions[] }}
 */
module.exports = pm2 || {
  apps: [
    {
      name: 'alemonb',
      script: 'node index.js',
      env: {
        NODE_ENV: 'production'
      }
    }
  ]
};
