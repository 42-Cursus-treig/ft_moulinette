#include <stdio.h>
#include <stdlib.h>

char *ft_strdup(char *src);

int main(int argc, char **argv)
{
	char *dup;

	if (argc != 2)
		return (0);
	dup = ft_strdup(argv[1]);
	if (dup == NULL)
		return (1);
	printf("%s", dup);
	free(dup);
	return (0);
}
