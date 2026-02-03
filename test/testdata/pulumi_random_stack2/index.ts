import * as random from "@pulumi/random";

const randomString = new random.RandomString("random", {
    length: 17,
    special: true,
    overrideSpecial: "/@£$",
  })