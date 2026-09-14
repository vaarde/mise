import {
  GetSecretValueCommand,
  SecretsManagerClient,
} from "@aws-sdk/client-secrets-manager";

export class CachedSecretValue {
  private cached: string | undefined;

  constructor(
    private readonly secretId: string,
    private readonly client = new SecretsManagerClient({}),
  ) {}

  async get(): Promise<string> {
    if (this.cached) return this.cached;
    const response = await this.client.send(
      new GetSecretValueCommand({ SecretId: this.secretId }),
    );
    const value = response.SecretString?.trim();
    if (!value) throw new Error(`secret has no string value: ${this.secretId}`);
    this.cached = value;
    return value;
  }
}
