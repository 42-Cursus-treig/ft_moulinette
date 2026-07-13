#include <stdio.h>
#include <stdlib.h>

char *ft_convert_base(char *nbr, char *base_from, char *base_to);

int main(int argc, char **argv)
{
	char *res;

	if (argc != 4)
		return (0);
	res = ft_convert_base(argv[1], argv[2], argv[3]);
	if (res == NULL)
	{
		printf("NULL");
		return (0);
	}
	printf("%s", res);
	free(res);
	return (0);
}
