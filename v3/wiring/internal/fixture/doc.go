/* Package fixture holds the input the wiring generator is measured on, and the output it produced.

Everything under this directory is DELIBERATELY exempt from the mirror rule of the test layout audit —
no <source>_test.go is written for these files, and the reason is measured rather than assumed:

  - domain/ and scoped/ are the constructors the generator SCANS. They carry no guard of their own; every
    property that matters about them — which constructor is found, which argument is bound, which build
    tag excludes one — is asserted in scanner_test.go and generator_test.go, against these files as data.
    A mirror here would test the fixture instead of the generator, which is the only thing under test.
  - wiring/ and wiringscoped/ are the generator's OUTPUT, carrying the "Code generated ... DO NOT EDIT"
    banner. They are regenerated rather than written, so a mirror beside them would be asserting the
    generator's product twice: renderer_test.go and generate_command_test.go already compare the rendered
    text against what the generator was asked to produce.

The files still compile and are still built, which is what makes them an honest fixture: a generated file
that no longer compiles fails the build here rather than in an application. */
package fixture
